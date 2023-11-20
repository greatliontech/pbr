package config

import (
	"os"

	"github.com/drone/envsubst"
	"github.com/gobwas/glob"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Credentials Credentials
	Modules     map[string]Module
	Plugins     map[string]Plugin
	Host        string
	Address     string
}

type Module struct {
	Remote  string
	Path    string
	Filters []string
	Replace bool
}

type Plugin struct {
	Registry string
	Image    string
}

type Credentials struct {
	Bsr map[string]string
	Git map[string]string
}

func (creds Credentials) BsrToken(remote string) string {
	return creds.Bsr[remote]
}

func (creds Credentials) GitToken(remote string) string {
	for k, v := range creds.Git {
		g := glob.MustCompile(k)
		if g.Match(remote) {
			return v
		}
	}
	return ""
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
		v, err := envsubst.EvalEnv(v)
		if err != nil {
			return nil, err
		}
		c.Credentials.Git[k] = v
	}
	if c.Address == "" {
		c.Address = ":443"
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
