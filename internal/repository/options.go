package repository

import (
	"github.com/go-git/go-git/v5/plumbing/transport"
)

type Option func(*Repository)

func WithAuth(auth transport.AuthMethod) Option {
	return func(r *Repository) {
		r.auth = auth
	}
}

func WithShallow() Option {
	return func(r *Repository) {
		r.shallow = true
	}
}
