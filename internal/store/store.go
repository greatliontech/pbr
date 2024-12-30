package store

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInternal      = errors.New("internal error")
)

type Store interface {
	UserStore
	TokenStore
	OwnerStore
	ModuleStore
	PluginStore
}

type User struct {
	Email string
}

type UserStore interface {
	CreateUser(ctx context.Context, user *User) (*User, error)
	DeleteUser(ctx context.Context, id string) error
}

type Token struct {
	Token  string
	UserID string
}

type TokenStore interface {
	CreateToken(ctx context.Context, token *Token) (*Token, error)
	DeleteToken(ctx context.Context, token string) error
	GetToken(ctx context.Context, token string) (*Token, error)
	GetTokensForUser(ctx context.Context, userID string) ([]*Token, error)
}

type Owner struct {
	ID   string
	Name string
}

type OwnerStore interface {
	CreateOwner(ctx context.Context, owner *Owner) (*Owner, error)
	DeleteOwner(ctx context.Context, id string) error
	GetOwner(ctx context.Context, id string) (*Owner, error)
	GetOwnerByName(ctx context.Context, name string) (*Owner, error)
	ListOwners(ctx context.Context) ([]*Owner, error)
}

type Module struct {
	ID      string
	OwnerID string
	Owner   string
	Name    string
	RepoURL string
	Root    string
	Filters []string
	Shallow bool
}

type ModuleStore interface {
	CreateModule(ctx context.Context, module *Module) (*Module, error)
	DeleteModule(ctx context.Context, id string) error
	GetModule(ctx context.Context, id string) (*Module, error)
	GetModuleByName(ctx context.Context, ownerID, name string) (*Module, error)
	ListModules(ctx context.Context, ownerID string) ([]*Module, error)
}

type Plugin struct {
	ID         string
	OwnerID    string
	Name       string
	ImageURL   string
	DefaultTag string
}

type PluginStore interface {
	CreatePlugin(ctx context.Context, plugin *Plugin) (*Plugin, error)
	DeletePlugin(ctx context.Context, id string) error
	GetPlugin(ctx context.Context, id string) (*Plugin, error)
	GetPluginByName(ctx context.Context, ownerID, name string) (*Plugin, error)
}
