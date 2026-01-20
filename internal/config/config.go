package config

import (
	"os"

	"github.com/drone/envsubst"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Credentials Credentials
	Modules     map[string]Module
	Plugins     map[string]Plugin
	Users       map[string]string
	TLS         *TLS
	Host        string
	Address     string
	LogLevel    string
	CacheDir    string
	AdminToken  string
	NoLogin     bool
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
	return c, nil
}

func FromFile(f string) (*Config, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	return ParseConfig(b)
}
