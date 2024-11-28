package secrets

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/greatliontech/pbr/pkg/repository"
	"gopkg.in/yaml.v3"
)

var ErrNotDirectory = errors.New("path is not a directory")

var ErrInvalidSecret = func(kind, field string) error {
	return errors.New("invalid " + kind + " secret: missing field " + field)
}

type Manager struct {
	fs fs.FS
}

func NewManager(path string) (*Manager, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		return nil, ErrNotDirectory
	}

	return &Manager{fs: os.DirFS(path)}, nil
}

func (m *Manager) GetGitBasic(name string) (*repository.BasicAuthProvider, error) {
	out := &repository.BasicAuthProvider{}
	if out.Username == "" {
		return nil, ErrInvalidSecret("basic", "username")
	}
	if out.Password == "" {
		return nil, ErrInvalidSecret("basic", "password")
	}
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) GetGitSSH(name string) (*repository.SSHAuthProvider, error) {
	out := &repository.SSHAuthProvider{}
	if out.Key == nil {
		return nil, ErrInvalidSecret("ssh", "key")
	}
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) GetGitToken(name string) (*repository.TokenAuthProvider, error) {
	out := &repository.TokenAuthProvider{}
	if out.Token == "" {
		return nil, ErrInvalidSecret("token", "token")
	}
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) GetGitGithubApp(name string) (*repository.GithubAppAuthProvider, error) {
	out := &repository.GithubAppAuthProvider{}
	if out.AppID == 0 {
		return nil, ErrInvalidSecret("github-app", "appId")
	}
	if out.InstallationID == 0 {
		return nil, ErrInvalidSecret("github-app", "installationId")
	}
	if out.PrivateKey == nil {
		return nil, ErrInvalidSecret("github-app", "privateKey")
	}
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) readAndUnmarshal(name string, out interface{}) error {
	f, err := m.fs.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
