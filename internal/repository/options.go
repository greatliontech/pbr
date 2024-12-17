package repository

import (
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
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

func WithShallow() Option {
	return func(r *Repository) {
		r.shallow = true
	}
}
