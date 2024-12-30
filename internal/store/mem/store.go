package mem

import (
	"context"

	"github.com/greatliontech/pbr/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("pbr.dev/internal/store/mem")

func New() store.Store {
	return &memStore{
		users:   SyncMap[string, *store.User]{},
		token:   SyncMap[string, *store.Token]{},
		owners:  SyncMap[string, *store.Owner]{},
		modules: SyncMap[string, *store.Module]{},
		plugins: SyncMap[string, *store.Plugin]{},
	}
}

var _ store.Store = &memStore{}

type memStore struct {
	users   SyncMap[string, *store.User]
	token   SyncMap[string, *store.Token]
	owners  SyncMap[string, *store.Owner]
	modules SyncMap[string, *store.Module]
	plugins SyncMap[string, *store.Plugin]
}

func (m *memStore) CreateUser(ctx context.Context, user *store.User) (*store.User, error) {
	res, ok := m.users.LoadOrStore(user.Email, user)
	if ok {
		return nil, store.ErrAlreadyExists
	}
	return res, nil
}

func (m *memStore) DeleteUser(ctx context.Context, id string) error {
	_, ok := m.users.LoadAndDelete(id)
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func (m *memStore) CreateToken(ctx context.Context, token *store.Token) (*store.Token, error) {
	res, ok := m.token.LoadOrStore(token.Token, token)
	if ok {
		return nil, store.ErrAlreadyExists
	}
	return res, nil
}

func (m *memStore) DeleteToken(ctx context.Context, token string) error {
	_, ok := m.token.LoadAndDelete(token)
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func (m *memStore) GetToken(ctx context.Context, token string) (*store.Token, error) {
	res, ok := m.token.Load(token)
	if !ok {
		return nil, store.ErrNotFound
	}
	return res, nil
}

func (m *memStore) GetTokensForUser(ctx context.Context, userID string) ([]*store.Token, error) {
	out := []*store.Token{}
	m.token.Range(func(k string, v *store.Token) bool {
		if v.UserID == userID {
			out = append(out, v)
			return false
		}
		return true
	})
	return out, nil
}

func (m *memStore) CreateOwner(ctx context.Context, owner *store.Owner) (*store.Owner, error) {
	ownerId := fakeUUID(owner.Name)
	owner.ID = ownerId
	res, ok := m.owners.LoadOrStore(ownerId, owner)
	if ok {
		return nil, store.ErrAlreadyExists
	}
	return res, nil
}

func (m *memStore) DeleteOwner(ctx context.Context, id string) error {
	_, ok := m.owners.LoadAndDelete(id)
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func (m *memStore) GetOwner(ctx context.Context, id string) (*store.Owner, error) {
	res, ok := m.owners.Load(id)
	if !ok {
		return nil, store.ErrNotFound
	}
	return res, nil
}

func (m *memStore) GetOwnerByName(ctx context.Context, name string) (*store.Owner, error) {
	ctx, span := tracer.Start(ctx, "GetOwnerByName", trace.WithAttributes(
		attribute.String("name", name),
	))
	defer span.End()

	var out *store.Owner
	m.owners.Range(func(k string, v *store.Owner) bool {
		if v.Name == name {
			out = v
			return false
		}
		return true
	})
	if out == nil {
		return nil, store.ErrNotFound
	}
	return out, nil
}

func (m *memStore) ListOwners(ctx context.Context) ([]*store.Owner, error) {
	out := []*store.Owner{}
	m.owners.Range(func(k string, v *store.Owner) bool {
		out = append(out, v)
		return true
	})
	return out, nil
}

func (m *memStore) CreateModule(ctx context.Context, module *store.Module) (*store.Module, error) {
	moduleId := fakeUUID(module.OwnerID + "/" + module.Name)
	module.ID = moduleId
	res, ok := m.modules.LoadOrStore(moduleId, module)
	if ok {
		return nil, store.ErrAlreadyExists
	}
	return res, nil
}

func (m *memStore) DeleteModule(ctx context.Context, id string) error {
	_, ok := m.modules.LoadAndDelete(id)
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func (m *memStore) GetModule(ctx context.Context, id string) (*store.Module, error) {
	res, ok := m.modules.Load(id)
	if !ok {
		return nil, store.ErrNotFound
	}
	return res, nil
}

func (m *memStore) GetModuleByName(ctx context.Context, ownerID string, name string) (*store.Module, error) {
	ctx, span := tracer.Start(ctx, "GetModuleByName", trace.WithAttributes(
		attribute.String("ownerID", ownerID),
		attribute.String("name", name),
	))
	defer span.End()

	moduleId := fakeUUID(ownerID + name)
	return m.GetModule(ctx, moduleId)
}

func (m *memStore) ListModules(ctx context.Context, ownerID string) ([]*store.Module, error) {
	out := []*store.Module{}
	m.modules.Range(func(k string, v *store.Module) bool {
		if v.OwnerID == ownerID || ownerID == "" {
			out = append(out, v)
		}
		return true
	})
	return out, nil
}

func (m *memStore) CreatePlugin(ctx context.Context, plugin *store.Plugin) (*store.Plugin, error) {
	plugId := fakeUUID(plugin.OwnerID + plugin.Name)
	plugin.ID = plugId
	res, ok := m.plugins.LoadOrStore(plugId, plugin)
	if ok {
		return nil, store.ErrAlreadyExists
	}
	return res, nil
}

func (m *memStore) DeletePlugin(ctx context.Context, id string) error {
	_, ok := m.plugins.LoadAndDelete(id)
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func (m *memStore) GetPlugin(ctx context.Context, id string) (*store.Plugin, error) {
	res, ok := m.plugins.Load(id)
	if !ok {
		return nil, store.ErrNotFound
	}
	return res, nil
}

func (m *memStore) GetPluginByName(ctx context.Context, ownerID string, name string) (*store.Plugin, error) {
	plugId := fakeUUID(ownerID + name)
	return m.GetPlugin(ctx, plugId)
}
