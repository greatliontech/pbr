package mem

import (
	"context"

	"github.com/greatliontech/pbr/internal/store"
)

func New() store.Store {
	return &memStore{
		users:   syncMap[string, *store.User]{},
		token:   syncMap[string, *store.Token]{},
		owners:  syncMap[string, *store.Owner]{},
		modules: syncMap[string, *store.Module]{},
		plugins: syncMap[string, *store.Plugin]{},
	}
}

var _ store.Store = &memStore{}

type memStore struct {
	users   syncMap[string, *store.User]
	token   syncMap[string, *store.Token]
	owners  syncMap[string, *store.Owner]
	modules syncMap[string, *store.Module]
	plugins syncMap[string, *store.Plugin]
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

func (m *memStore) CreateModule(ctx context.Context, module *store.Module) (*store.Module, error) {
	moduleId := fakeUUID(module.OwnerID + module.Name)
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
	moduleId := fakeUUID(ownerID + name)
	return m.GetModule(ctx, moduleId)
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
