package repository

import (
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

type Option func(*Repository)

func WithAuth(auth transport.AuthMethod) Option {
	slog.Debug("repository with auth")
	return func(r *Repository) {
		r.auth = auth
	}
}

func WithShallow() Option {
	slog.Debug("repository with shallow")
	return func(r *Repository) {
		r.shallow = true
	}
}
