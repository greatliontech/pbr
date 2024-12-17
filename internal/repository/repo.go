package repository

import (
	"fmt"
	"log/slog"
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
	shallow     bool
}

type File struct {
	Name    string
	SHA     string
	Content string
}

func NewRepository(url, path string, opts ...Option) *Repository {
	slog.Debug("new repository", "url", url, "path", path)
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

func (r *Repository) Files(trgtRef, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	depth := 0
	if r.shallow {
		depth = 1
	}

	var ref *plumbing.Reference

	if trgtRef == "" {
		err := r.remote.Fetch(&git.FetchOptions{
			Depth: depth,
			RefSpecs: []config.RefSpec{
				config.RefSpec("+HEAD:HEAD"),
			},
			Force: true,
			Auth:  r.auth,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, nil, err
		}
		ref, err = r.storer.Reference(plumbing.NewBranchReferenceName("HEAD"))
		if err != nil {
			return nil, nil, err
		}
	} else {
		refspec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", trgtRef, trgtRef))
		err := r.remote.Fetch(&git.FetchOptions{
			Depth: depth,
			RefSpecs: []config.RefSpec{
				refspec,
			},
			Force: true,
			Auth:  r.auth,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			// try fetch tag
			fmt.Println("fetching tag")
			refspec = config.RefSpec(fmt.Sprintf("+refs/tags/%s:refs/tags/%s", trgtRef, trgtRef))
			err := r.remote.Fetch(&git.FetchOptions{
				Depth: depth,
				RefSpecs: []config.RefSpec{
					refspec,
				},
				Force: true,
				Auth:  r.auth,
			})
			if err != nil && err != git.NoErrAlreadyUpToDate {
				return nil, nil, err
			}
		}
		ref, err = r.storer.Reference(plumbing.ReferenceName(refspec.Src()))
		if err != nil {
			return nil, nil, err
		}
	}

	return r.files(ref.Hash(), root, filters...)
}

func (r *Repository) FilesCommit(cmmt, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	var h plumbing.Hash

	if !r.shallow {
		err := r.remote.Fetch(&git.FetchOptions{
			RefSpecs: []config.RefSpec{
				config.RefSpec("+HEAD:HEAD"),
			},
			Tags:  git.NoTags,
			Force: true,
			Auth:  r.auth,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, nil, err
		}
		iter, err := r.storer.IterEncodedObjects(plumbing.CommitObject)
		if err != nil {
			return nil, nil, err
		}
		err = iter.ForEach(func(eo plumbing.EncodedObject) error {
			if strings.HasPrefix(eo.Hash().String(), cmmt) {
				h = eo.Hash()
			}
			return nil
		})
	} else {
		// get all remote refs
		refs, err := r.remote.List(&git.ListOptions{
			Auth:          r.auth,
			PeelingOption: git.AppendPeeled,
		})
		if err != nil {
			return nil, nil, err
		}

		var rf *plumbing.Reference
		for _, ref := range refs {
			if strings.HasPrefix(ref.Hash().String(), cmmt) {
				rf = ref
				h = ref.Hash()
			}
		}
		if rf != nil {
			err = r.remote.Fetch(&git.FetchOptions{
				Depth: 1,
				RefSpecs: []config.RefSpec{
					config.RefSpec(fmt.Sprintf("+%s:%s", rf.Name().String(), rf.Name().String())),
				},
				Force: true,
				Auth:  r.auth,
			})
			if err != nil && err != git.NoErrAlreadyUpToDate {
				return nil, nil, err
			}
		}
	}

	if h.IsZero() {
		return nil, nil, fmt.Errorf("commit not found: %s", cmmt)
	}
	return r.files(h, root, filters...)
}

func (r *Repository) files(cmmt plumbing.Hash, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	// get the commit
	commit, err := object.GetCommit(r.storer, cmmt)
	if err != nil {
		return nil, nil, err
	}

	// get the tree
	tree, err := commit.Tree()
	if err != nil {
		return nil, nil, err
	}

	// chroot if requested
	if root != "" {
		root, err := tree.FindEntry(root)
		if err != nil {
			return nil, nil, err
		}
		if root.Mode != filemode.Dir {
			return nil, nil, fmt.Errorf("root path is not a directory")
		}
		tree, err = object.GetTree(r.storer, root.Hash)
		if err != nil {
			return nil, nil, err
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

	return commit, files, nil
}
