package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/drone/envsubst"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Credentials Credentials
	Modules     map[string]Module
	Plugins     map[string]Plugin
	Users       map[string]string
	TLS         *TLS
	Storage     *Storage
	OIDC        *OIDC
	Host        string
	Address     string
	LogLevel    string
	CacheDir    string
	AdminToken  string
	NoLogin     bool
	// TokenTTL is the duration for which OIDC tokens are valid (e.g., "7d", "24h", "168h").
	// Tokens are refreshed on each use (sliding expiration). Default: "7d" (7 days).
	TokenTTL string `yaml:"token_ttl"`
}

// OIDC configures OpenID Connect authentication.
// When configured, users can authenticate via an external identity provider.
type OIDC struct {
	// Issuer is the OIDC issuer URL (e.g., "https://keycloak.example.com/realms/myrealm")
	Issuer string `yaml:"issuer"`
	// ClientID is the OIDC client ID registered with the provider
	ClientID string `yaml:"client_id"`
	// ClientSecret is the OIDC client secret (supports ${ENV_VAR} substitution)
	ClientSecret string `yaml:"client_secret"`
	// Scopes are the OIDC scopes to request (default: ["openid", "email", "profile"])
	Scopes []string `yaml:"scopes"`
	// UsernameClaim is the claim to use as the username (default: "preferred_username")
	UsernameClaim string `yaml:"username_claim"`
}

// Storage configures the backend storage using gocloud.dev URLs.
// See https://gocloud.dev/howto/blob/ and https://gocloud.dev/howto/docstore/ for URL formats.
type Storage struct {
	// BlobURL is the gocloud.dev blob URL (e.g., "file:///path/to/blobs", "s3://bucket", "gs://bucket")
	BlobURL string `yaml:"blob_url"`
	// DocstoreURL is the gocloud.dev docstore URL (e.g., "mem://", "firestore://project/collection")
	// For memdocstore, use "mem://" - data will be persisted to CacheDir/docstore on shutdown
	DocstoreURL string `yaml:"docstore_url"`
}

type Module struct {
	Remote  string
	Path    string
	Filters []string
	Shallow bool
}

type TLS struct {
	CertFile string // Path to certificate file
	KeyFile  string // Path to key file
	CertPEM  string // Raw certificate PEM (supports ${ENV_VAR} substitution)
	KeyPEM   string // Raw key PEM (supports ${ENV_VAR} substitution)
}

type Plugin struct {
	Image   string
	Default string
}

type BasicGitAuth struct {
	Username string
	Password string
}

type GithubAppGitAuth struct {
	AppID          int64
	InstallationID int64
	PrivateKey     string
}

type GitAuth struct {
	Token     string
	SSHKey    string
	Basic     *BasicGitAuth
	GithubApp *GithubAppGitAuth
}

type ContainerRegistryAuth struct {
	Username      string
	Password      string
	Auth          string
	IdentityToken string
	RegistryToken string
}

type Credentials struct {
	// Key is glob
	Git map[string]GitAuth
	// Key is prefix
	ContainerRegistry map[string]ContainerRegistryAuth
}

func ParseConfig(b []byte) (*Config, error) {
	c := &Config{}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	for k, v := range c.Credentials.Git {
		// ssh key env secret
		sshKey, err := envsubst.EvalEnv(v.SSHKey)
		if err != nil {
			return nil, err
		}
		v.SSHKey = sshKey
		// token env secret
		token, err := envsubst.EvalEnv(v.Token)
		if err != nil {
			return nil, err
		}
		v.Token = token
		// gh app env secret
		if v.GithubApp != nil {
			key, err := envsubst.EvalEnv(v.GithubApp.PrivateKey)
			if err != nil {
				return nil, err
			}
			v.GithubApp.PrivateKey = key
		}
		c.Credentials.Git[k] = v
	}
	tkn, err := envsubst.EvalEnv(c.AdminToken)
	if err != nil {
		return nil, err
	}
	c.AdminToken = tkn
	for k, v := range c.Users {
		v, err := envsubst.EvalEnv(v)
		if err != nil {
			return nil, err
		}
		c.Users[k] = v
	}
	for k, v := range c.Credentials.ContainerRegistry {
		v.Password, err = envsubst.EvalEnv(v.Password)
		if err != nil {
			return nil, err
		}
		v.Auth, err = envsubst.EvalEnv(v.Auth)
		if err != nil {
			return nil, err
		}
		v.IdentityToken, err = envsubst.EvalEnv(v.IdentityToken)
		if err != nil {
			return nil, err
		}
		v.RegistryToken, err = envsubst.EvalEnv(v.RegistryToken)
		if err != nil {
			return nil, err
		}
		c.Credentials.ContainerRegistry[k] = v
	}
	// TLS PEM env substitution
	if c.TLS != nil {
		if c.TLS.CertPEM != "" {
			c.TLS.CertPEM, err = envsubst.EvalEnv(c.TLS.CertPEM)
			if err != nil {
				return nil, err
			}
		}
		if c.TLS.KeyPEM != "" {
			c.TLS.KeyPEM, err = envsubst.EvalEnv(c.TLS.KeyPEM)
			if err != nil {
				return nil, err
			}
		}
	}
	// Storage URL env substitution
	if c.Storage != nil {
		if c.Storage.BlobURL != "" {
			c.Storage.BlobURL, err = envsubst.EvalEnv(c.Storage.BlobURL)
			if err != nil {
				return nil, err
			}
		}
		if c.Storage.DocstoreURL != "" {
			c.Storage.DocstoreURL, err = envsubst.EvalEnv(c.Storage.DocstoreURL)
			if err != nil {
				return nil, err
			}
		}
	}
	// OIDC env substitution
	if c.OIDC != nil {
		if c.OIDC.ClientSecret != "" {
			c.OIDC.ClientSecret, err = envsubst.EvalEnv(c.OIDC.ClientSecret)
			if err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func FromFile(f string) (*Config, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	return ParseConfig(b)
}

// DefaultTokenTTL is the default token time-to-live (7 days).
const DefaultTokenTTL = 7 * 24 * time.Hour

// GetTokenTTL returns the configured token TTL duration.
// If not configured or invalid, returns DefaultTokenTTL.
func (c *Config) GetTokenTTL() time.Duration {
	if c.TokenTTL == "" {
		return DefaultTokenTTL
	}
	d, err := ParseDuration(c.TokenTTL)
	if err != nil {
		return DefaultTokenTTL
	}
	return d
}

// ParseDuration parses a duration string with support for days (e.g., "7d", "24h", "1d12h").
// Supports: "ns", "us", "ms", "s", "m", "h", "d" (days).
func ParseDuration(s string) (time.Duration, error) {
	// Handle days specially since time.ParseDuration doesn't support them
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Check if there's a 'd' for days
	if idx := strings.Index(s, "d"); idx != -1 {
		// Extract the number before 'd'
		daysPart := s[:idx]
		days, err := strconv.Atoi(daysPart)
		if err != nil {
			return 0, err
		}
		remainder := s[idx+1:]
		var remainderDuration time.Duration
		if remainder != "" {
			remainderDuration, err = time.ParseDuration(remainder)
			if err != nil {
				return 0, err
			}
		}
		return time.Duration(days)*24*time.Hour + remainderDuration, nil
	}

	// No days, use standard parsing
	return time.ParseDuration(s)
}
