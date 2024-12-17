package module

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/internal/repository"
	"golang.org/x/crypto/sha3"
)

type Module struct {
	Owner  string
	Module string
	Repo   *repository.Repository

	SHAKE256Cache map[string]string
	root          string
	filters       []glob.Glob
}

type File struct {
	Name     string
	SHA      string
	Content  string
	SHAKE256 string
}

type Manifest struct {
	Commit   string
	Content  string
	SHAKE256 string
}

func New(owner, module string, repo *repository.Repository, root string, filters []glob.Glob) *Module {
	return &Module{
		Owner:         owner,
		Module:        module,
		Repo:          repo,
		root:          root,
		filters:       filters,
		SHAKE256Cache: make(map[string]string),
	}
}

func (m *Module) FilesAndManifest(ref string) ([]File, *Manifest, error) {
	slog.Debug("module files and manifest", "ref", ref)
	filters, err := m.compileFilters()
	if err != nil {
		return nil, nil, err
	}

	commit, repoFiles, err := m.Repo.Files(ref, m.root, filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndManifest(commit, repoFiles)
}

func (m *Module) FilesAndManifestCommit(cmmt string) ([]File, *Manifest, error) {
	filters, err := m.compileFilters()
	if err != nil {
		return nil, nil, err
	}

	commit, repoFiles, err := m.Repo.FilesCommit(cmmt, m.root, filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndManifest(commit, repoFiles)
}

func (m *Module) filesAndManifest(commit *object.Commit, repoFiles []repository.File) ([]File, *Manifest, error) {
	var files []File
	var manifestContentBuilder strings.Builder

	for _, file := range repoFiles {
		shake256Hash, ok := m.SHAKE256Cache[file.SHA]
		if !ok {
			// calculate SHAKE256 hash
			h := sha3.NewShake256()
			_, err := h.Write([]byte(file.Content))
			if err != nil {
				return nil, nil, err
			}
			var shake256Sum [64]byte
			h.Read(shake256Sum[:])
			shake256Hash = fmt.Sprintf("%x", shake256Sum)
			m.SHAKE256Cache[file.SHA] = shake256Hash
		}

		files = append(files, File{
			Name:     file.Name,
			SHA:      file.SHA,
			Content:  file.Content,
			SHAKE256: shake256Hash,
		})

		// add file info to the manifest content
		manifestContentBuilder.WriteString(fmt.Sprintf("shake256:%s  %s\n", files[len(files)-1].SHAKE256, file.Name))
	}

	// generate manifest
	manifestContent := manifestContentBuilder.String()
	manifest := &Manifest{
		Commit:  commit.Hash.String(),
		Content: manifestContent,
	}

	// calculate SHAKE256 hash for the manifest content
	h := sha3.NewShake256()
	_, err := h.Write([]byte(manifestContent))
	if err != nil {
		return nil, nil, err
	}
	var shake256Sum [64]byte
	h.Read(shake256Sum[:])
	manifest.SHAKE256 = fmt.Sprintf("%x", shake256Sum)

	return files, manifest, nil
}

func (m *Module) compileFilters() ([]glob.Glob, error) {
	filters := []glob.Glob{}
	fiterStrings := []string{
		"**.proto",
		"buf.yaml",
		"buf.lock",
	}
	for _, f := range fiterStrings {
		filter, err := glob.Compile(f)
		if err != nil {
			return nil, err
		}
		filters = append(filters, filter)
	}
	return append(filters, m.filters...), nil
}
