package codegen

import (
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"syscall"

	"github.com/greatliontech/container"
	"github.com/greatliontech/ocifs"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

type Plugin struct {
	ofs    *ocifs.OCIFS
	image  string
	defVer string
}

func NewPlugin(ofs *ocifs.OCIFS, image, defaultVersion string) *Plugin {
	return &Plugin{
		ofs:    ofs,
		image:  image,
		defVer: defaultVersion,
	}
}

func (p *Plugin) Image() string {
	return p.image
}

func (p *Plugin) CodeGen(ver string, in *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	img := p.Image()
	if ver == "" && p.defVer != "" {
		ver = p.defVer
	}
	if ver != "" {
		img = fmt.Sprintf("%s:%s", img, ver)
	}

	// mount the image
	im, err := p.ofs.Mount(img)
	if err != nil {
		slog.Error("failed to mount image", "msg", err)
		return nil, err
	}
	defer im.Unmount()
	defer os.Remove(im.MountPoint())

	// get config
	conf, err := im.ConfigFile()
	if err != nil {
		return nil, err
	}

	// get the entrypoint
	entrypoint := conf.Config.Entrypoint
	if len(entrypoint) == 0 {
		return nil, fmt.Errorf("no entrypoint found")
	}

	trgtroot, err := os.MkdirTemp(os.TempDir(), "trgt")
	if err != nil {
		slog.Error("failed to create trgt temp dir", "msg", err)
		return nil, err
	}
	defer os.RemoveAll(trgtroot)

	contId := randStringBytes(16)

	cfg := container.Config{
		Root:     trgtroot,
		Hostname: contId,
		Namespaces: container.Namespaces{
			NewIPC:  true,
			NewMnt:  true,
			NewNet:  true,
			NewPID:  true,
			NewUTS:  true,
			NewUser: true,
		},
		Mounts: []container.Mount{
			{
				Source: im.MountPoint(),
				Target: trgtroot,
				Type:   "auto",
				Flags:  syscall.MS_BIND | syscall.MS_RDONLY,
			},
		},
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      syscall.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      syscall.Getgid(),
				Size:        1,
			},
		},
	}

	cont, err := container.New("/tmp/contstate", contId, cfg)
	if err != nil {
		slog.Error("failed to create container", "msg", err)
		return nil, err
	}

	args := []string{}
	if len(entrypoint) > 1 {
		args = entrypoint[1:]
	}
	if len(conf.Config.Cmd) > 0 {
		args = append(args, conf.Config.Cmd...)
	}

	pr := &container.Process{
		Cmd:        entrypoint[0],
		Args:       args,
		StdinPipe:  true,
		StdoutPipe: true,
		StderrPipe: true,
	}

	if err := cont.Run(pr); err != nil {
		slog.Error("failed to run", "msg", err)
		return nil, err
	}

	stdin, err := cont.StdinPipe()
	if err != nil {
		slog.Error("failed to get stdin pipe", "msg", err)
		return nil, err
	}

	stdout, err := cont.StdoutPipe()
	if err != nil {
		slog.Error("failed to get stdout pipe", "msg", err)
		return nil, err
	}

	stderr, err := cont.StderrPipe()
	if err != nil {
		slog.Error("failed to get stderr pipe", "msg", err)
		return nil, err
	}

	slog.Info("running", "cmd", entrypoint[0])

	// Marshal the input to protobuf binary format
	inData, err := proto.Marshal(in)
	if err != nil {
		slog.Error("failed to marshal input", "msg", err)
		return nil, err
	}

	// Write the marshaled data to stdin
	if _, err := stdin.Write(inData); err != nil {
		slog.Error("failed to write to stdin", "msg", err)
		return nil, err
	}
	stdin.Close()

	// Read the output from stdout
	outData, err := io.ReadAll(stdout)
	if err != nil {
		return nil, err
	}

	// Also read from stderr
	errData, err := io.ReadAll(stderr)
	if err != nil {
		return nil, err
	}

	// Print anything that was sent to stderr
	if len(errData) > 0 {
		fmt.Printf("stderr: %s\n", string(errData))
		return nil, fmt.Errorf("stderr: %s", string(errData))
	}

	// Wait for the command to finish
	if err := cont.Wait(); err != nil {
		return nil, err
	}

	if len(outData) == 0 {
		return nil, fmt.Errorf("no output data")
	}

	// Unmarshal the output into a CodeGeneratorResponse
	out := &pluginpb.CodeGeneratorResponse{}
	if err := proto.Unmarshal(outData, out); err != nil {
		return nil, err
	}

	return out, nil
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
