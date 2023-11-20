package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/gobwas/glob"
	"golang.org/x/crypto/sha3"
)

type Repository struct {
	Repo          *git.Repository
	Path          string
	LastFetch     time.Time
	FetchPeriod   time.Duration
	token         string
	SHAKE256Cache map[string]string // Cache SHAKE256 hashes
	filters       []glob.Glob
	httpAuth      *http.BasicAuth
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

// New clones the repository and returns a new Repository struct
func New(repoUrl, token string, fetchPeriodSec int) (*Repository, error) {
	// determine cache directory
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		cacheHome = filepath.Join(home, ".cache")
	}

	// construct the path from the URL
	urlParts := strings.Split(repoUrl, "/")
	repoPath := filepath.Join(cacheHome, "pbr", strings.Join(urlParts[len(urlParts)-2:], "/"))

	// construct the HTTP auth struct
	var httpAuth *http.BasicAuth
	if token != "" {
		httpAuth = &http.BasicAuth{
			Username: "git",
			Password: token,
		}
	}

	// check if the repository already exists
	r, err := git.PlainOpen(repoPath)
	if err == git.ErrRepositoryNotExists {
		// repository does not exist, clone it
		r, err = git.PlainClone(repoPath, true, &git.CloneOptions{
			URL:        repoUrl,
			Auth:       httpAuth,
			Tags:       git.AllTags,
			NoCheckout: true,
		})
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		// some other error occurred while opening the repository
		return nil, err
	} else {
		// Repository exists, perform a fetch
		err = r.Fetch(&git.FetchOptions{
			Auth:     httpAuth,
			Tags:     git.AllTags,
			RefSpecs: []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, err
		}
	}

	return &Repository{
		Repo:          r,
		Path:          repoPath,
		LastFetch:     time.Now(),
		FetchPeriod:   time.Duration(fetchPeriodSec) * time.Second,
		token:         token,
		SHAKE256Cache: make(map[string]string),
		httpAuth:      httpAuth,
	}, nil
}

// FilesAndManifest retrieves all files and their SHAs for a given ref
func (repo *Repository) FilesAndManifest(ref string) ([]File, *Manifest, error) {
	// Check if fetch is needed
	if time.Since(repo.LastFetch) > repo.FetchPeriod {
		// Perform a fetch
		err := repo.Repo.Fetch(&git.FetchOptions{
			Auth:     repo.httpAuth,
			Tags:     git.AllTags,
			RefSpecs: []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, nil, err
		}
		repo.LastFetch = time.Now()
	}

	var hash plumbing.Hash

	// If ref is empty, use the default branch
	if ref == "" {
		head, err := repo.Repo.Head()
		if err != nil {
			return nil, nil, err
		}
		hash = head.Hash()
	} else {
		// Look for the ref in heads, then tags
		var err error
		hash, err = repo.findRefHash(ref)
		if err != nil {
			return nil, nil, err
		}
	}

	// Get the commit object
	commit, err := repo.Repo.CommitObject(hash)
	if err != nil {
		return nil, nil, err
	}

	var files []File
	var manifestContentBuilder strings.Builder

	filesItr, err := commit.Files()
	if err != nil {
		return nil, nil, err
	}

	err = filesItr.ForEach(func(f *object.File) error {
		// trim the repository path from the file name, if any
		name := strings.TrimPrefix(f.Name, repo.Path)

		// check if the file matches any of the filters, if any
		matched := true
		if len(repo.filters) > 0 {
			matched = false
		}
		for _, filter := range repo.filters {
			if filter.Match(name) {
				matched = true
			}
		}
		if !matched {
			return nil
		}

		// get file contents
		content, err := f.Contents()
		if err != nil {
			return err
		}

		sha := f.Hash.String()
		shake256Hash, ok := repo.SHAKE256Cache[sha]
		if !ok {
			// calculate SHAKE256 hash
			h := sha3.NewShake256()
			_, err := h.Write([]byte(content))
			if err != nil {
				return err
			}
			var shake256Sum [64]byte
			h.Read(shake256Sum[:])
			shake256Hash = fmt.Sprintf("%x", shake256Sum)
			repo.SHAKE256Cache[sha] = shake256Hash
		}

		files = append(files, File{
			Name:     f.Name,
			SHA:      sha,
			Content:  content,
			SHAKE256: shake256Hash,
		})

		// add file info to the manifest content
		manifestContentBuilder.WriteString(fmt.Sprintf("shake256:%s  %s\n", files[len(files)-1].SHAKE256, f.Name))

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// generate manifest
	manifestContent := manifestContentBuilder.String()
	manifest := &Manifest{
		Commit:  commit.Hash.String(),
		Content: manifestContent,
	}

	// calculate SHAKE256 hash for the manifest content
	h := sha3.NewShake256()
	_, err = h.Write([]byte(manifestContent))
	if err != nil {
		return nil, nil, err
	}
	var shake256Sum [64]byte
	h.Read(shake256Sum[:])
	manifest.SHAKE256 = fmt.Sprintf("%x", shake256Sum)

	return files, manifest, nil
}

// findRefHash attempts to find the hash for a given ref (sha, branch or tag)
func (repo *Repository) findRefHash(ref string) (plumbing.Hash, error) {
	// first, check if the ref is a valid commit SHA
	if hash := plumbing.NewHash(ref); hash != plumbing.ZeroHash {
		// Check if this hash corresponds to a valid commit
		_, err := repo.Repo.CommitObject(hash)
		if err == nil {
			// The commit exists, return its hash
			return hash, nil
		}
	}

	var hash plumbing.Hash
	var err error

	// check if it's a remote tracking branch
	remoteBranchRef := plumbing.NewRemoteReferenceName("origin", ref)
	remoteBranch, err := repo.Repo.Reference(remoteBranchRef, true)
	if err == nil {
		return remoteBranch.Hash(), nil
	}

	// check if it's a tag
	tagRef := plumbing.NewTagReferenceName(ref)
	tag, err := repo.Repo.Reference(tagRef, true)
	if err == nil {
		return tag.Hash(), nil
	}

	// check if it's a local branch
	branchRef := plumbing.NewBranchReferenceName(ref)
	branch, err := repo.Repo.Reference(branchRef, true)
	if err == nil {
		return branch.Hash(), nil
	}

	return hash, fmt.Errorf("reference not found: %s", ref)
}
