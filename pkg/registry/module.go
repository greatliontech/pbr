package registry

import (
	"context"
	"fmt"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
)

// Get Modules by id or name.
func (reg *Registry) GetModules(ctx context.Context, req *connect.Request[v1.GetModulesRequest]) (*connect.Response[v1.GetModulesResponse], error) {
	resp := &connect.Response[v1.GetModulesResponse]{
		Msg: &v1.GetModulesResponse{},
	}

	for _, ref := range req.Msg.ModuleRefs {
		switch ref := ref.Value.(type) {
		case *v1.ModuleRef_Id:
			mod := reg.moduleIds[ref.Id]
			resp.Msg.Modules = append(resp.Msg.Modules, &v1.Module{
				Id:      ref.Id,
				Name:    mod.Module,
				OwnerId: fakeUUID(mod.Owner),
			})
		case *v1.ModuleRef_Name_:
			fmt.Println("GetModules error", "ModuleRef_Name_ not supported", ref)
		}
	}

	return resp, nil
}

// List Modules, usually for a specific User or Organization.
func (reg *Registry) ListModules(_ context.Context, _ *connect.Request[v1.ListModulesRequest]) (*connect.Response[v1.ListModulesResponse], error) {
	panic("not implemented") // TODO: Implement
}

// Create new Modules.
//
// When a Module is created, a Branch representing the release Branch
// is created as well.
//
// This operation is atomic. Either all Modules are created or an error is returned.
func (reg *Registry) CreateModules(_ context.Context, _ *connect.Request[v1.CreateModulesRequest]) (*connect.Response[v1.CreateModulesResponse], error) {
	panic("not implemented") // TODO: Implement
}

// Update existing Modules.
//
// This operation is atomic. Either all Modules are updated or an error is returned.
func (reg *Registry) UpdateModules(_ context.Context, _ *connect.Request[v1.UpdateModulesRequest]) (*connect.Response[v1.UpdateModulesResponse], error) {
	panic("not implemented") // TODO: Implement
}

// Delete existing Modules.
//
// This operation is atomic. Either all Modules are deleted or an error is returned.
func (reg *Registry) DeleteModules(_ context.Context, _ *connect.Request[v1.DeleteModulesRequest]) (*connect.Response[v1.DeleteModulesResponse], error) {
	panic("not implemented") // TODO: Implement
}
