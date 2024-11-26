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
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) GetGitSSH(name string) (*repository.SSHAuthProvider, error) {
	out := &repository.SSHAuthProvider{}
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) GetGitToken(name string) (*repository.TokenAuthProvider, error) {
	out := &repository.TokenAuthProvider{}
	err := m.readAndUnmarshal(name, out)
	return out, err
}

func (m *Manager) GetGitGithubApp(name string) (*repository.GithubAppAuthProvider, error) {
	out := &repository.GithubAppAuthProvider{}
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
