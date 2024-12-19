package repository

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/pkg/config"
)

type CredentialStore struct {
	creds map[*glob.Glob]AuthProvider
}

func NewCredentialStore(creds map[string]config.GitAuth) (*CredentialStore, error) {
	fmt.Println("new cred store", creds)
	cs := &CredentialStore{
		creds: map[*glob.Glob]AuthProvider{},
	}
	for k, v := range creds {
		g, err := glob.Compile(k)
		if err != nil {
			return nil, err
		}
		if v.SSHKey != "" {
			cs.creds[&g] = &SSHAuthProvider{
				Key: []byte(v.SSHKey),
			}
		}
		if v.Token != "" {
			cs.creds[&g] = &TokenAuthProvider{
				Token: v.Token,
			}
		}
		if v.GithubApp != nil {
			cs.creds[&g] = &GithubAppAuthProvider{
				AppID:          v.GithubApp.AppID,
				InstallationID: v.GithubApp.InstallationID,
				PrivateKey:     []byte(v.GithubApp.PrivateKey),
			}
		}
	}
	return cs, nil
}

func (cs *CredentialStore) Auth(remote string) (transport.AuthMethod, error) {
	for k, v := range cs.creds {
		g := *k
		if g.Match(remote) {
			fmt.Println("using creds for remote:", remote)
			return v.AuthMethod()
		}
	}
	return nil, nil
}
