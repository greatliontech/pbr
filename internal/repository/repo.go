package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("pbr.dev/internal/repository")

var ErrCommitNotFound = errors.New("commit not found")

type Repository struct {
	path        string
	auth        AuthProvider
	lastFetch   map[string]time.Time
	fetchPeriod time.Duration
	mu          sync.Mutex
	remote      *git.Remote
	storer      storage.Storer
	shallow     bool

	// commitIdCache is a generic wrapper around sync.Map
	commitIdCache util.SyncMap[string, *object.Commit]
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
		path:        path,
		remote:      rmt,
		storer:      strg,
		auth:        auth,
		shallow:     shallow,
		fetchPeriod: 1 * time.Minute,
		lastFetch:   map[string]time.Time{},
	}
}

// Delete removes the repository directory from disk.
func (r *Repository) Delete() error {
	return os.RemoveAll(r.path)
}

// Files fetches and returns the commit and files for a given branch/tag reference.
// If trgtRef is empty, it fetches HEAD.
func (r *Repository) Files(ctx context.Context, trgtRef, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	ctx, span := tracer.Start(ctx, "Repository.Files", trace.WithAttributes(
		attribute.String("ref", trgtRef),
		attribute.String("root", root),
	))
	defer span.End()

	depth := 0
	if r.shallow {
		depth = 1
	}

	auth, err := r.getAuth()
	if err != nil {
		return nil, nil, err
	}

	var ref *plumbing.Reference
	if trgtRef == "" {
		// Fetch HEAD
		if err := r.fetchRef(ctx, "+HEAD:HEAD", depth, auth); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to fetch HEAD")
			return nil, nil, err
		}

		ref, err = r.storer.Reference(plumbing.NewBranchReferenceName("HEAD"))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to get HEAD reference")
			return nil, nil, err
		}
	} else {
		// Fetch the branch reference first
		refSpec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", trgtRef, trgtRef))
		err := r.fetchRef(ctx, refSpec.String(), depth, auth)
		if err != nil && err != git.NoErrAlreadyUpToDate {
			// If branch fetch fails, try fetching the tag
			refSpec = config.RefSpec(fmt.Sprintf("+refs/tags/%s:refs/tags/%s", trgtRef, trgtRef))
			if tagErr := r.fetchRef(ctx, refSpec.String(), depth, auth); tagErr != nil && tagErr != git.NoErrAlreadyUpToDate {
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
func (r *Repository) FilesCommit(ctx context.Context, cmmt, root string, filters ...glob.Glob) (*object.Commit, []File, error) {
	ctx, span := tracer.Start(ctx, "Repository.FilesCommit", trace.WithAttributes(
		attribute.String("commitId", cmmt),
		attribute.String("root", root),
	))
	defer span.End()

	auth, err := r.getAuth()
	if err != nil {
		return nil, nil, err
	}

	var h plumbing.Hash
	if !r.shallow {
		// Fetch all commits for HEAD (no tags)
		err := r.fetchRef(ctx, "+HEAD:HEAD", 0, auth)
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
			if err := r.fetchRef(ctx, refSpec.String(), 1, auth); err != nil && err != git.NoErrAlreadyUpToDate {
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
func (r *Repository) CommitFromShort(ctx context.Context, cmmt string) (*object.Commit, error) {
	ctx, span := tracer.Start(ctx, "Repository.CommitFromShort", trace.WithAttributes(
		attribute.String("commitId", cmmt),
	))
	defer span.End()

	slog.DebugContext(ctx, "CommitFromShort", "commitId", cmmt)

	// Check the cache
	if cmt, ok := r.commitIdCache.Load(cmmt); ok {
		slog.DebugContext(ctx, "CommitFromShort", "cache", "hit", "commitId", cmt.Hash.String())
		return cmt, nil
	}

	slog.DebugContext(ctx, "CommitFromShort", "cache", "miss", "commitId", cmmt)
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
	auth, authErr := r.getAuth()
	if authErr != nil {
		return nil, authErr
	}

	if r.shallow {
		// For shallow clone, try to find a matching remote reference
		refs, listErr := r.remote.List(&git.ListOptions{Auth: auth})
		if listErr != nil {
			slog.DebugContext(ctx, "remote.List", "err", listErr)
			return nil, listErr
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref.Hash().String(), cmmt) {
				refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", ref.Name().String(), ref.Name().String()))
				fetchErr := r.fetchRef(ctx, refSpec.String(), 1, auth)
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
		if err := r.fetchRef(ctx, "+HEAD:HEAD", 0, auth); err != nil && err != git.NoErrAlreadyUpToDate {
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

	return nil, ErrCommitNotFound
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
func (r *Repository) fetchRef(ctx context.Context, refSpec string, depth int, auth transport.AuthMethod) error {
	ctx, span := tracer.Start(ctx, "Repository.fetchRef", trace.WithAttributes(
		attribute.String("refSpec", refSpec),
		attribute.Int("depth", depth),
	))
	defer span.End()
	r.mu.Lock()
	defer r.mu.Unlock()
	lf, ok := r.lastFetch[refSpec]
	if ok && time.Since(lf) < r.fetchPeriod {
		span.AddEvent("skipping fetch, within period")
		return nil
	}
	span.AddEvent("fetching")
	err := r.remote.Fetch(&git.FetchOptions{
		Depth:    depth,
		RefSpecs: []config.RefSpec{config.RefSpec(refSpec)},
		Force:    true,
		Auth:     auth,
	})
	r.lastFetch[refSpec] = time.Now()
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}
	return nil
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

func (r *Repository) getAuth() (transport.AuthMethod, error) {
	if r.auth == nil {
		return nil, nil
	}
	return r.auth.AuthMethod()
}
