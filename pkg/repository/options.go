package repository

import (
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/gobwas/glob"
)

type Option func(*Repository)

func WithAuth(auth transport.AuthMethod) Option {
	return func(r *Repository) {
		r.auth = auth
	}
}

func WithSyncPeriod(period int) Option {
	return func(r *Repository) {
		r.fetchPeriod = time.Duration(period) * time.Second
	}
}

func WithFilters(filters []string) Option {
	return func(r *Repository) {
		r.filters = make([]glob.Glob, len(filters))
		for i, f := range filters {
			r.filters[i] = glob.MustCompile(f)
		}
	}
}

func WithRoot(root string) Option {
	return func(r *Repository) {
		r.root = root
	}
}
