package repository

import (
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/pkg/config"
)

type CredentialStore struct {
	creds map[*glob.Glob]transport.AuthMethod
}

func NewCredentialStore(creds map[string]config.GitAuth) (*CredentialStore, error) {
	cs := &CredentialStore{
		creds: map[*glob.Glob]transport.AuthMethod{},
	}
	for k, v := range creds {
		g, err := glob.Compile(k)
		if err != nil {
			return nil, err
		}
		if v.SSHKey != "" {
			publicKeys, err := ssh.NewPublicKeys("git", []byte(v.SSHKey), "")
			if err != nil {
				return nil, err
			}
			cs.creds[&g] = publicKeys
		}
		if v.Token != "" {
			cs.creds[&g] = &http.BasicAuth{
				Username: "git",
				Password: v.Token,
			}
		}

	}
	return cs, nil
}

func (cs *CredentialStore) Auth(remote string) transport.AuthMethod {
	for k, v := range cs.creds {
		g := *k
		if g.Match(remote) {
			return v
		}
	}
	return nil
}
