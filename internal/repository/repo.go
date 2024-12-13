package repository

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/gobwas/glob"
)

type Repository struct {
	path        string
	auth        transport.AuthMethod
	lastFetch   map[string]time.Time
	fetchPeriod time.Duration
	remote      *git.Remote
	storer      storage.Storer
}

type File struct {
	Name    string
	SHA     string
	Content string
}

func NewRepository(url, path string, opts ...Option) *Repository {
	csh := &cache.ObjectLRU{
		MaxSize: 50 * cache.KiByte,
	}
	strg := filesystem.NewStorage(osfs.New(path), csh)
	rmt := git.NewRemote(strg, &config.RemoteConfig{
		URLs: []string{url},
	})
	repo := &Repository{
		path:   path,
		remote: rmt,
		storer: strg,
	}
	// apply options
	for _, opt := range opts {
		opt(repo)
	}
	return repo
}

func (r *Repository) Delete() error {
	return os.RemoveAll(r.path)
}

func (r *Repository) Files(trgtRef, root string, filters ...glob.Glob) ([]File, error) {
	// get all remote refs
	refs, err := r.remote.List(&git.ListOptions{
		Auth: r.auth,
	})
	if err != nil {
		return nil, err
	}

	branchName := plumbing.NewBranchReferenceName(trgtRef)
	if trgtRef == "" {
		branchName = plumbing.HEAD
	}
	tagName := plumbing.NewTagReferenceName(trgtRef)

	// find target ref
	var trgt *plumbing.Reference
	for _, ref := range refs {
		if ref.Name() == branchName || ref.Name() == tagName {
			trgt = ref
			break
		}
		if trgtRef != "" && strings.HasSuffix(ref.Hash().String(), trgtRef) {
			trgt = ref
			break
		}
	}
	if trgt == nil {
		return nil, fmt.Errorf("reference not found: %s", trgtRef)
	}

	rmtName := "refs/remotes/origin/" + trgt.Name().Short()
	err = r.remote.Fetch(&git.FetchOptions{
		Depth: 1,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+%s:%s", trgt.Name(), rmtName)),
		},
		Force: true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, err
	}

	if trgtRef == "" {
		trgt, err = r.storer.Reference(plumbing.ReferenceName(rmtName))
		if err != nil {
			return nil, err
		}
	}

	var commit *object.Commit
	commit, err = object.GetCommit(r.storer, trgt.Hash())
	if err != nil {
		// try get annotated tag
		tag, err := object.GetTag(r.storer, trgt.Hash())
		if err != nil {
			return nil, err
		}
		commit, err = tag.Commit()
		if err != nil {
			return nil, err
		}
	}

	// get the tree
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	// chroot if requested
	if root != "" {
		root, err := tree.FindEntry(root)
		if err != nil {
			return nil, err
		}
		if root.Mode != filemode.Dir {
			return nil, fmt.Errorf("root path is not a directory")
		}
		tree, err = object.GetTree(r.storer, root.Hash)
		if err != nil {
			return nil, err
		}
	}

	files := make([]File, 0)

	err = tree.Files().ForEach(func(f *object.File) error {
		// filter file
		matched := true
		if len(filters) > 0 {
			matched = false
			for _, filter := range filters {
				if filter.Match(f.Name) {
					matched = true
					break
				}
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

		files = append(files, File{
			Name:    f.Name,
			SHA:     sha,
			Content: content,
		})

		return nil
	})

	return files, nil
}
