package registry

import (
	"context"
	"fmt"
	"strings"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
)

// Get Modules by id or name.
func (reg *Registry) GetModules(ctx context.Context, req *connect.Request[v1.GetModulesRequest]) (*connect.Response[v1.GetModulesResponse], error) {
	fmt.Println("GetModules")
	resp := connect.NewResponse(&v1.GetModulesResponse{})

	for _, ref := range req.Msg.ModuleRefs {
		switch ref := ref.Value.(type) {
		case *v1.ModuleRef_Id:
			mod, ok := reg.moduleIds[ref.Id]
			if !ok {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("Module not found: %s", ref.Id))
			}
			ownerId := strings.Split(mod, "/")[0]
			modName := strings.Split(mod, "/")[1]
			if !ok {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("Owner not found: %s", ownerId))
			}
			resp.Msg.Modules = append(resp.Msg.Modules, &v1.Module{
				Id:      ref.Id,
				Name:    modName,
				OwnerId: ownerId,
			})
		case *v1.ModuleRef_Name_:
			fmt.Println("GetModules error", "ModuleRef_Name_ not supported", ref)
		}
	}

	return resp, nil
}

// List Modules, usually for a specific User or Organization.
func (reg *Registry) ListModules(_ context.Context, _ *connect.Request[v1.ListModulesRequest]) (*connect.Response[v1.ListModulesResponse], error) {
	resp := connect.NewResponse(&v1.ListModulesResponse{})
	return resp, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("ListModules not implemented"))
}

// Create new Modules.
//
// When a Module is created, a Branch representing the release Branch
// is created as well.
//
// This operation is atomic. Either all Modules are created or an error is returned.
func (reg *Registry) CreateModules(_ context.Context, _ *connect.Request[v1.CreateModulesRequest]) (*connect.Response[v1.CreateModulesResponse], error) {
	resp := connect.NewResponse(&v1.CreateModulesResponse{})
	return resp, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("CreateModules not implemented"))
}

// Update existing Modules.
//
// This operation is atomic. Either all Modules are updated or an error is returned.
func (reg *Registry) UpdateModules(_ context.Context, _ *connect.Request[v1.UpdateModulesRequest]) (*connect.Response[v1.UpdateModulesResponse], error) {
	resp := connect.NewResponse(&v1.UpdateModulesResponse{})
	return resp, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("UpdateModules not implemented"))
}

// Delete existing Modules.
//
// This operation is atomic. Either all Modules are deleted or an error is returned.
func (reg *Registry) DeleteModules(_ context.Context, _ *connect.Request[v1.DeleteModulesRequest]) (*connect.Response[v1.DeleteModulesResponse], error) {
	resp := connect.NewResponse(&v1.DeleteModulesResponse{})
	return resp, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("DeleteModules not implemented"))
}
