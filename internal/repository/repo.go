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
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/internal/store/mem"
)

type Repository struct {
	path        string
	auth        AuthProvider
	lastFetch   map[string]time.Time
	fetchPeriod time.Duration
	remote      *git.Remote
	storer      storage.Storer
	shallow     bool

	// commitIdCache is a generic wrapper around sync.Map
	commitIdCache mem.SyncMap[string, *object.Commit]
}

type File struct {
	Name    string
	SHA     string
	Content string
}

// NewRepository initializes a new Repository instance with an in-filesystem storage.
func NewRepository(url, path string, auth AuthProvider, shallow bool) *Repository {
	csh := &cache.ObjectLRU{
		MaxSize: 50 * cache.KiByte,
	}
	strg := filesystem.NewStorage(osfs.New(path), csh)
	rmt := git.NewRemote(strg, &config.RemoteConfig{
		URLs: []string{url},
	})

	return &Repository{
		path:    path,
		remote:  rmt,
		storer:  strg,
		auth:    auth,
		shallow: shallow,
	}
}

// Delete removes the repository directory from disk.
func (r *Repository) Delete() error {
	return os.RemoveAll(r.path)
}

// Files fetches and returns the commit and files for a given branch/tag reference.
// If trgtRef is empty, it fetches HEAD.
func (r *Repository) Files(trgtRef, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	depth := 0
	if r.shallow {
		depth = 1
	}

	auth, err := r.auth.AuthMethod()
	if err != nil {
		return nil, nil, err
	}

	var ref *plumbing.Reference
	if trgtRef == "" {
		// Fetch HEAD
		if err := r.fetchRef("+HEAD:HEAD", depth, auth); err != nil {
			return nil, nil, err
		}

		ref, err = r.storer.Reference(plumbing.NewBranchReferenceName("HEAD"))
		if err != nil {
			return nil, nil, err
		}
	} else {
		// Fetch the branch reference first
		refSpec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", trgtRef, trgtRef))
		err := r.fetchRef(refSpec.String(), depth, auth)
		if err != nil && err != git.NoErrAlreadyUpToDate {
			// If branch fetch fails, try fetching the tag
			fmt.Println("fetching tag:", trgtRef)
			refSpec = config.RefSpec(fmt.Sprintf("+refs/tags/%s:refs/tags/%s", trgtRef, trgtRef))
			if tagErr := r.fetchRef(refSpec.String(), depth, auth); tagErr != nil && tagErr != git.NoErrAlreadyUpToDate {
				return nil, nil, tagErr
			}
		}

		// Attempt to lookup reference by branch name first
		ref, err = r.storer.Reference(plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", trgtRef)))
		if err == plumbing.ErrReferenceNotFound {
			// If branch not found, try as a tag
			ref, err = r.storer.Reference(plumbing.ReferenceName(fmt.Sprintf("refs/tags/%s", trgtRef)))
		}
		if err != nil {
			return nil, nil, err
		}
	}

	return r.files(ref.Hash(), root, filters...)
}

// FilesCommit fetches and returns the commit and files for a given commit hash.
// If repository is shallow, it tries to find a matching remote ref and fetches only depth=1.
func (r *Repository) FilesCommit(cmmt, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	auth, err := r.auth.AuthMethod()
	if err != nil {
		return nil, nil, err
	}

	var h plumbing.Hash
	if !r.shallow {
		// Fetch all commits for HEAD (no tags)
		err := r.fetchRef("+HEAD:HEAD", 0, auth)
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, nil, err
		}

		// Look for local commit that matches the short hash
		h, err = r.findLocalCommitHash(cmmt)
		if err != nil && err != plumbing.ErrObjectNotFound {
			return nil, nil, err
		}
	} else {
		// Shallow: we must list remote refs, find a matching ref, and fetch it
		refs, listErr := r.remote.List(&git.ListOptions{Auth: auth})
		if listErr != nil {
			return nil, nil, listErr
		}

		var matchedRef *plumbing.Reference
		for _, ref := range refs {
			if strings.HasPrefix(ref.Hash().String(), cmmt) {
				matchedRef = ref
				h = ref.Hash()
				break
			}
		}
		if matchedRef != nil {
			// Fetch only that ref at depth=1
			refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", matchedRef.Name().String(), matchedRef.Name().String()))
			if err := r.fetchRef(refSpec.String(), 1, auth); err != nil && err != git.NoErrAlreadyUpToDate {
				return nil, nil, err
			}
		}
	}

	// If not found yet, try once more in local objects after fetching
	if h.IsZero() {
		// Attempt local lookup again
		foundHash, err := r.findLocalCommitHash(cmmt)
		if err != nil {
			return nil, nil, fmt.Errorf("commit not found: %s", cmmt)
		}
		h = foundHash
	}

	return r.files(h, root, filters...)
}

// CommitFromShort returns a full commit object from a short commit hash.
func (r *Repository) CommitFromShort(cmmt string) (*object.Commit, error) {
	// Check the cache
	if cmt, ok := r.commitIdCache.Load(cmmt); ok {
		return cmt, nil
	}

	// Local lookup
	localCommit, err := r.localLookupShortSha(cmmt)
	if err == nil {
		r.commitIdCache.Store(cmmt, localCommit)
		return localCommit, nil
	}
	if err != plumbing.ErrObjectNotFound {
		// If it's a different error, return it
		return nil, err
	}

	// Not found locally => attempt to fetch, if shallow or not
	auth, authErr := r.auth.AuthMethod()
	if authErr != nil {
		return nil, authErr
	}

	if r.shallow {
		// For shallow clone, try to find a matching remote reference
		refs, listErr := r.remote.List(&git.ListOptions{Auth: auth})
		if listErr != nil {
			return nil, listErr
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref.Hash().String(), cmmt) {
				refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", ref.Name().String(), ref.Name().String()))
				fetchErr := r.fetchRef(refSpec.String(), 1, auth)
				if fetchErr != nil && fetchErr != git.NoErrAlreadyUpToDate {
					return nil, fetchErr
				}
				// After fetching, try local lookup again
				localCommit, err = r.localLookupShortSha(cmmt)
				if err == nil {
					r.commitIdCache.Store(cmmt, localCommit)
					return localCommit, nil
				}
				if err != plumbing.ErrObjectNotFound {
					return nil, err
				}
			}
		}
	} else {
		// Non-shallow: fetch HEAD forcibly
		if err := r.fetchRef("+HEAD:HEAD", 0, auth); err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, err
		}
		// After fetching, try local lookup again
		localCommit, err = r.localLookupShortSha(cmmt)
		if err == nil {
			r.commitIdCache.Store(cmmt, localCommit)
			return localCommit, nil
		}
		if err != plumbing.ErrObjectNotFound {
			return nil, err
		}
	}

	return nil, fmt.Errorf("commit not found after fetch: %s", cmmt)
}

// localLookupShortSha searches for a commit in the local object database by short hash.
func (r *Repository) localLookupShortSha(cid string) (*object.Commit, error) {
	iter, err := r.storer.IterEncodedObjects(plumbing.CommitObject)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var h plumbing.Hash
	err = iter.ForEach(func(eo plumbing.EncodedObject) error {
		if strings.HasPrefix(eo.Hash().String(), cid) {
			h = eo.Hash()
			return storer.ErrStop
		}
		return nil
	})
	if err != nil && err != storer.ErrStop {
		return nil, err
	}
	if h.IsZero() {
		return nil, plumbing.ErrObjectNotFound
	}

	return object.GetCommit(r.storer, h)
}

// files retrieves the commit, reads the tree (optionally chrooted by root path),
// and filters files by the provided globs.
func (r *Repository) files(cmmt plumbing.Hash, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	commit, err := object.GetCommit(r.storer, cmmt)
	if err != nil {
		return nil, nil, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, nil, err
	}

	// If a root path is specified, descend into that directory
	if root != "" {
		rootEntry, err := tree.FindEntry(root)
		if err != nil {
			return nil, nil, err
		}
		if rootEntry.Mode != filemode.Dir {
			return nil, nil, fmt.Errorf("root path is not a directory")
		}
		tree, err = object.GetTree(r.storer, rootEntry.Hash)
		if err != nil {
			return nil, nil, err
		}
	}

	var files []File
	err = tree.Files().ForEach(func(f *object.File) error {
		// Skip if file doesn’t match any of the globs
		if len(filters) > 0 {
			match := false
			for _, flt := range filters {
				if flt.Match(f.Name) {
					match = true
					break
				}
			}
			if !match {
				return nil
			}
		}

		content, err := f.Contents()
		if err != nil {
			return err
		}

		files = append(files, File{
			Name:    f.Name,
			SHA:     f.Hash.String(),
			Content: content,
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return commit, files, nil
}

// fetchRef is a small helper to DRY up repeated fetch logic.
func (r *Repository) fetchRef(refSpec string, depth int, auth transport.AuthMethod) error {
	return r.remote.Fetch(&git.FetchOptions{
		Depth:    depth,
		RefSpecs: []config.RefSpec{config.RefSpec(refSpec)},
		Force:    true,
		Auth:     auth,
	})
}

// findLocalCommitHash iterates through local commits to find one that starts with
// the provided short commit string. Returns ErrObjectNotFound if none matches.
func (r *Repository) findLocalCommitHash(shortHash string) (plumbing.Hash, error) {
	iter, err := r.storer.IterEncodedObjects(plumbing.CommitObject)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer iter.Close()

	var h plumbing.Hash
	err = iter.ForEach(func(eo plumbing.EncodedObject) error {
		if strings.HasPrefix(eo.Hash().String(), shortHash) {
			h = eo.Hash()
			return storer.ErrStop
		}
		return nil
	})

	if err != nil && err != storer.ErrStop {
		return plumbing.ZeroHash, err
	}
	if h.IsZero() {
		return plumbing.ZeroHash, plumbing.ErrObjectNotFound
	}
	return h, nil
}
