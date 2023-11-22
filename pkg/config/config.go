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
}

type Module struct {
	Remote  string
	Path    string
	Filters []string
	Replace bool
}

type TLS struct {
	CertFile string
	KeyFile  string
}

type Plugin struct {
	Image string
}

type GitAuth struct {
	Token      string
	SSHKey     string
	SSHKeyFile string
}

type Credentials struct {
	Bsr map[string]string
	Git map[string]GitAuth
}

func ParseConfig(b []byte) (*Config, error) {
	c := &Config{}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	for k, v := range c.Credentials.Bsr {
		v, err := envsubst.EvalEnv(v)
		if err != nil {
			return nil, err
		}
		c.Credentials.Bsr[k] = v
	}
	for k, v := range c.Credentials.Git {
		sshKey, err := envsubst.EvalEnv(v.SSHKey)
		if err != nil {
			return nil, err
		}
		v.SSHKey = sshKey
		token, err := envsubst.EvalEnv(v.Token)
		if err != nil {
			return nil, err
		}
		v.Token = token
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
