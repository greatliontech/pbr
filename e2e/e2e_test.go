package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	registryHost = "pbr.test"
	envoyImage   = "envoyproxy/envoy:v1.28-latest"
	bufImage     = "bufbuild/buf:latest"
)

type tlsMode int

const (
	tlsModeEnvoy tlsMode = iota // Envoy terminates TLS, proxies to PBR HTTP
	tlsModeNative               // PBR handles TLS directly
)

func (m tlsMode) String() string {
	switch m {
	case tlsModeEnvoy:
		return "Envoy"
	case tlsModeNative:
		return "NativeTLS"
	default:
		return "Unknown"
	}
}

type testEnv struct {
	network        *testcontainers.DockerNetwork
	pbrContainer   testcontainers.Container
	envoyContainer testcontainers.Container
	certsDir       string
	testdataDir    string
	registryHost   string
	mode           tlsMode
}

func setupTestEnv(t *testing.T, mode tlsMode, registryHost string) *testEnv {
	t.Helper()
	ctx := context.Background()

	// Create temp dir for certs
	certsDir, err := os.MkdirTemp("", "pbr-e2e-certs-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Generate certificates
	if err := generateCerts(certsDir, registryHost); err != nil {
		os.RemoveAll(certsDir)
		t.Fatalf("failed to generate certs: %v", err)
	}

	// Get testdata directory
	wd, _ := os.Getwd()
	testdataDir := filepath.Join(wd, "testdata")

	// Create Docker network
	net, err := network.New(ctx)
	if err != nil {
		os.RemoveAll(certsDir)
		t.Fatalf("failed to create network: %v", err)
	}

	env := &testEnv{
		network:      net,
		certsDir:     certsDir,
		testdataDir:  testdataDir,
		registryHost: registryHost,
		mode:         mode,
	}

	switch mode {
	case tlsModeEnvoy:
		env.setupEnvoyMode(t, ctx, registryHost, certsDir)
	case tlsModeNative:
		env.setupNativeTLSMode(t, ctx, registryHost, certsDir)
	}

	return env
}

func (e *testEnv) setupEnvoyMode(t *testing.T, ctx context.Context, registryHost, certsDir string) {
	t.Helper()

	// Create envoy config
	envoyConfig := generateEnvoyConfig(registryHost)
	envoyConfigPath := filepath.Join(certsDir, "envoy.yaml")
	if err := os.WriteFile(envoyConfigPath, []byte(envoyConfig), 0644); err != nil {
		e.cleanup(ctx)
		t.Fatalf("failed to write envoy config: %v", err)
	}

	// Create PBR config (HTTP, no TLS)
	pbrConfig := fmt.Sprintf(`host: %s
address: ":8080"
loglevel: debug
cachedir: /data
nologin: true
`, registryHost)
	pbrConfigPath := filepath.Join(certsDir, "config.yaml")
	if err := os.WriteFile(pbrConfigPath, []byte(pbrConfig), 0644); err != nil {
		e.cleanup(ctx)
		t.Fatalf("failed to write pbr config: %v", err)
	}

	// Build PBR image from current source
	pbrContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "Dockerfile",
			},
			Networks:       []string{e.network.Name},
			NetworkAliases: map[string][]string{e.network.Name: {"pbr"}},
			ExposedPorts:   []string{"8080/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: pbrConfigPath, ContainerFilePath: "/config/config.yaml", FileMode: 0644},
			},
			Env: map[string]string{
				"PBR_DEBUG_HTTP": "1",
			},
			WaitingFor: wait.ForHTTP("/readyz").WithPort("8080/tcp").WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		e.cleanup(ctx)
		t.Fatalf("failed to start PBR container: %v", err)
	}
	e.pbrContainer = pbrContainer

	// Start Envoy container for TLS termination
	envoyContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          envoyImage,
			Networks:       []string{e.network.Name},
			NetworkAliases: map[string][]string{e.network.Name: {registryHost}},
			ExposedPorts:   []string{"443/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: envoyConfigPath, ContainerFilePath: "/etc/envoy/envoy.yaml", FileMode: 0644},
				{HostFilePath: filepath.Join(certsDir, "server.crt"), ContainerFilePath: "/etc/envoy/server.crt", FileMode: 0644},
				{HostFilePath: filepath.Join(certsDir, "server.key"), ContainerFilePath: "/etc/envoy/server.key", FileMode: 0644},
			},
			Cmd:        []string{"-c", "/etc/envoy/envoy.yaml"},
			WaitingFor: wait.ForListeningPort("443/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		e.cleanup(ctx)
		t.Fatalf("failed to start Envoy container: %v", err)
	}
	e.envoyContainer = envoyContainer
}

func (e *testEnv) setupNativeTLSMode(t *testing.T, ctx context.Context, registryHost, certsDir string) {
	t.Helper()

	// Create PBR config with TLS
	pbrConfig := fmt.Sprintf(`host: %s
address: ":443"
loglevel: debug
cachedir: /data
nologin: true
tls:
  certfile: /certs/server.crt
  keyfile: /certs/server.key
`, registryHost)
	pbrConfigPath := filepath.Join(certsDir, "config.yaml")
	if err := os.WriteFile(pbrConfigPath, []byte(pbrConfig), 0644); err != nil {
		e.cleanup(ctx)
		t.Fatalf("failed to write pbr config: %v", err)
	}

	// Build PBR image with TLS enabled
	pbrContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "Dockerfile",
			},
			Networks:       []string{e.network.Name},
			NetworkAliases: map[string][]string{e.network.Name: {registryHost}},
			ExposedPorts:   []string{"443/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: pbrConfigPath, ContainerFilePath: "/config/config.yaml", FileMode: 0644},
				{HostFilePath: filepath.Join(certsDir, "server.crt"), ContainerFilePath: "/certs/server.crt", FileMode: 0644},
				{HostFilePath: filepath.Join(certsDir, "server.key"), ContainerFilePath: "/certs/server.key", FileMode: 0644},
			},
			Env: map[string]string{
				"PBR_DEBUG_HTTP": "1",
			},
			WaitingFor: wait.ForHTTP("/readyz").WithPort("443/tcp").WithTLS(true).WithAllowInsecure(true).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		e.cleanup(ctx)
		t.Fatalf("failed to start PBR container with TLS: %v", err)
	}
	e.pbrContainer = pbrContainer
}

func (e *testEnv) cleanup(ctx context.Context) {
	if e.envoyContainer != nil {
		e.envoyContainer.Terminate(ctx)
	}
	if e.pbrContainer != nil {
		e.pbrContainer.Terminate(ctx)
	}
	if e.network != nil {
		e.network.Remove(ctx)
	}
	if e.certsDir != "" {
		os.RemoveAll(e.certsDir)
	}
}

func (e *testEnv) runBuf(t *testing.T, ctx context.Context, workDir string, args ...string) (string, error) {
	t.Helper()

	// Get current user ID to run container as same user (avoids permission issues with buf.lock)
	uid := os.Getuid()
	gid := os.Getgid()

	// Run buf CLI in container
	req := testcontainers.ContainerRequest{
		Image:    bufImage,
		Networks: []string{e.network.Name},
		Cmd:      args,
		User:     fmt.Sprintf("%d:%d", uid, gid),
		Files: []testcontainers.ContainerFile{
			{HostFilePath: filepath.Join(e.certsDir, "ca.crt"), ContainerFilePath: "/etc/ssl/certs/ca.crt", FileMode: 0644},
		},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Mounts = []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: workDir,
					Target: "/workspace",
				},
			}
		},
		WorkingDir: "/workspace",
		Env: map[string]string{
			"SSL_CERT_FILE": "/etc/ssl/certs/ca.crt",
			"HOME":          "/tmp", // Writable dir for buf cache when running as non-root
		},
		WaitingFor: wait.ForExit(),
	}

	bufContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to run buf container: %w", err)
	}
	defer bufContainer.Terminate(ctx)

	// Get logs
	logs, err := bufContainer.Logs(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	logBytes, _ := io.ReadAll(logs)
	output := string(logBytes)

	// Check exit code
	state, err := bufContainer.State(ctx)
	if err != nil {
		return output, fmt.Errorf("failed to get state: %w", err)
	}
	if state.ExitCode != 0 {
		return output, fmt.Errorf("buf exited with code %d: %s", state.ExitCode, output)
	}

	return output, nil
}

func (e *testEnv) runBufExpectSuccess(t *testing.T, ctx context.Context, workDir string, args ...string) string {
	t.Helper()
	output, err := e.runBuf(t, ctx, workDir, args...)
	if err != nil {
		t.Fatalf("buf %s failed: %v", strings.Join(args, " "), err)
	}
	return output
}

func (e *testEnv) runBufExpectFailure(t *testing.T, ctx context.Context, workDir string, args ...string) string {
	t.Helper()
	output, err := e.runBuf(t, ctx, workDir, args...)
	if err == nil {
		t.Fatalf("buf %s should have failed", strings.Join(args, " "))
	}
	return output
}

func (e *testEnv) dumpPBRLogs(t *testing.T, ctx context.Context) {
	t.Helper()
	logs, err := e.pbrContainer.Logs(ctx)
	if err != nil {
		t.Logf("failed to get PBR logs: %v", err)
		return
	}
	defer logs.Close()
	logBytes, _ := io.ReadAll(logs)
	t.Logf("PBR server logs:\n%s", string(logBytes))
}

// runTestSuite runs the common test suite against an environment
func runTestSuite(t *testing.T, env *testEnv) {
	ctx := context.Background()

	t.Run("BasicModule", func(t *testing.T) {
		dir := filepath.Join(env.testdataDir, "basic")

		t.Run("lint", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "lint")
		})

		t.Run("build", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "build")
		})

		t.Run("push", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})

		t.Run("module_info", func(t *testing.T) {
			out := env.runBufExpectSuccess(t, ctx, dir, "registry", "module", "info", env.registryHost+"/e2e/basic")
			if !strings.Contains(out, "basic") {
				t.Errorf("expected 'basic' in output: %s", out)
			}
		})
	})

	t.Run("ModuleWithDependencies", func(t *testing.T) {
		// Ensure basic is pushed first
		basicDir := filepath.Join(env.testdataDir, "basic")
		env.runBufExpectSuccess(t, ctx, basicDir, "push", "--create")

		dir := filepath.Join(env.testdataDir, "deps")

		t.Run("dep_update", func(t *testing.T) {
			output, err := env.runBuf(t, ctx, dir, "dep", "update", "--debug")
			if err != nil {
				env.dumpPBRLogs(t, ctx)
				t.Fatalf("buf dep update --debug failed: %v\nOutput: %s", err, output)
			}
		})

		t.Run("build", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "build")
		})

		t.Run("push", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})
	})

	t.Run("Labels", func(t *testing.T) {
		dir := filepath.Join(env.testdataDir, "labels")

		t.Run("push_main", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})

		t.Run("push_v1.0.0", func(t *testing.T) {
			env.runBufExpectSuccess(t, ctx, dir, "push", "--label", "v1.0.0")
		})
	})

	// Test nested dependencies with diamond pattern:
	//         top
	//        /   \
	//    mid-a   mid-b
	//        \   /
	//         base
	t.Run("NestedDependencies", func(t *testing.T) {
		nestedDir := filepath.Join(env.testdataDir, "nested")

		// Push base module first (leaf dependency)
		t.Run("push_base", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "base")
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})

		// Push base again with v1.0.0 label
		t.Run("push_base_v1", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "base")
			env.runBufExpectSuccess(t, ctx, dir, "push", "--label", "v1.0.0")
		})

		// Push mid-a (depends on base)
		t.Run("push_mid_a", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "mid-a")
			env.runBufExpectSuccess(t, ctx, dir, "dep", "update")
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})

		// Push mid-b (also depends on base)
		t.Run("push_mid_b", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "mid-b")
			env.runBufExpectSuccess(t, ctx, dir, "dep", "update")
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})

		// Test top module (depends on both mid-a and mid-b, creating diamond)
		t.Run("top_dep_update", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "top")
			output, err := env.runBuf(t, ctx, dir, "dep", "update", "--debug")
			if err != nil {
				env.dumpPBRLogs(t, ctx)
				t.Fatalf("buf dep update --debug failed: %v\nOutput: %s", err, output)
			}
		})

		t.Run("top_build", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "top")
			env.runBufExpectSuccess(t, ctx, dir, "build")
		})

		t.Run("top_push", func(t *testing.T) {
			dir := filepath.Join(nestedDir, "top")
			env.runBufExpectSuccess(t, ctx, dir, "push", "--create")
		})
	})

	t.Run("ErrorCases", func(t *testing.T) {
		dir := env.testdataDir

		t.Run("nonexistent_module", func(t *testing.T) {
			env.runBufExpectFailure(t, ctx, dir, "registry", "module", "info", env.registryHost+"/nonexistent/module")
		})
	})
}

// TestE2EEnvoyTLS tests with Envoy doing TLS termination
func TestE2EEnvoyTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	ctx := context.Background()
	env := setupTestEnv(t, tlsModeEnvoy, registryHost)
	defer env.cleanup(ctx)

	runTestSuite(t, env)
}

// TestE2ENativeTLS tests with PBR handling TLS directly
func TestE2ENativeTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	ctx := context.Background()
	env := setupTestEnv(t, tlsModeNative, registryHost)
	defer env.cleanup(ctx)

	runTestSuite(t, env)
}

// generateCerts creates CA and server certificates
func generateCerts(dir, host string) error {
	// Generate CA
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PBR Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caCert, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	// Save CA cert
	if err := savePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", caCert); err != nil {
		return err
	}
	if err := savePEM(filepath.Join(dir, "ca.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey)); err != nil {
		return err
	}

	// Generate server cert
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}

	serverCert, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	if err := savePEM(filepath.Join(dir, "server.crt"), "CERTIFICATE", serverCert); err != nil {
		return err
	}
	if err := savePEM(filepath.Join(dir, "server.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey)); err != nil {
		return err
	}

	return nil
}

func savePEM(path, typ string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: data})
}

func generateEnvoyConfig(host string) string {
	return fmt.Sprintf(`static_resources:
  listeners:
    - name: listener_0
      address:
        socket_address:
          address: 0.0.0.0
          port_value: 443
      filter_chains:
        - filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                codec_type: AUTO
                stat_prefix: ingress_http
                http2_protocol_options: {}
                http_filters:
                  - name: envoy.filters.http.router
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
                route_config:
                  name: local_route
                  virtual_hosts:
                    - name: local_service
                      domains: ["%s"]
                      routes:
                        - match:
                            prefix: "/"
                          route:
                            cluster: backend_service
                            timeout: 0s
          transport_socket:
            name: envoy.transport_sockets.tls
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext
              common_tls_context:
                alpn_protocols: ["h2", "http/1.1"]
                tls_certificates:
                  certificate_chain:
                    filename: "/etc/envoy/server.crt"
                  private_key:
                    filename: "/etc/envoy/server.key"
  clusters:
    - name: backend_service
      connect_timeout: 30s
      type: STRICT_DNS
      lb_policy: ROUND_ROBIN
      typed_extension_protocol_options:
        envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
          "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
          explicit_http_config:
            http2_protocol_options: {}
      load_assignment:
        cluster_name: backend_service
        endpoints:
          - lb_endpoints:
              - endpoint:
                  address:
                    socket_address:
                      address: pbr
                      port_value: 8080
`, host)
}
