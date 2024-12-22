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
	TLS         *TLS
	Host        string
	Address     string
	LogLevel    string
	CacheDir    string
	AdminToken  string
}

type Module struct {
	Remote  string
	Path    string
	Filters []string
	Shallow bool
}

type TLS struct {
	CertFile string
	KeyFile  string
}

type Plugin struct {
	Image    string
	Registry string
	// Format is a go template string that will be used to format the target image.
	// If empty, the result will be <registry>/{{.Owner}}/{{.Repository}}.
	Format string
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

type Credentials struct {
	Git               map[string]GitAuth
	ContainerRegistry map[string]string
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
	return c, nil
}

func FromFile(f string) (*Config, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	return ParseConfig(b)
}
