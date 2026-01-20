package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	ownerv1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ModuleServiceV1 implements the v1 ModuleService interface by wrapping Service.
type ModuleServiceV1 struct {
	svc *Service
}

// NewModuleServiceV1 creates a new v1 ModuleService wrapper.
func NewModuleServiceV1(svc *Service) *ModuleServiceV1 {
	return &ModuleServiceV1{svc: svc}
}

// GetModules retrieves modules by id or name (v1 API).
func (m *ModuleServiceV1) GetModules(ctx context.Context, req *connect.Request[v1.GetModulesRequest]) (*connect.Response[v1.GetModulesResponse], error) {
	if m.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	resp := connect.NewResponse(&v1.GetModulesResponse{})

	for _, ref := range req.Msg.ModuleRefs {
		var mod *v1.Module
		var err error

		switch r := ref.Value.(type) {
		case *v1.ModuleRef_Id:
			mod, err = m.getModuleByID(ctx, r.Id)
		case *v1.ModuleRef_Name_:
			if r.Name != nil {
				mod, err = m.getModuleByName(ctx, r.Name.Owner, r.Name.Module)
			} else {
				err = errors.New("module name is nil")
			}
		default:
			err = errors.New("unknown module ref type")
		}

		if err != nil {
			return nil, err
		}

		resp.Msg.Modules = append(resp.Msg.Modules, mod)
	}

	return resp, nil
}

func (m *ModuleServiceV1) getModuleByID(ctx context.Context, id string) (*v1.Module, error) {
	mod, err := m.svc.casReg.ModuleByID(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s", id))
	}

	return &v1.Module{
		Id:          mod.ID(),
		OwnerId:     mod.OwnerID(),
		Name:        mod.Name(),
		Description: mod.Description(),
		CreateTime:  timestamppb.New(mod.CreateTime()),
	}, nil
}

func (m *ModuleServiceV1) getModuleByName(ctx context.Context, owner, name string) (*v1.Module, error) {
	mod, err := m.svc.casReg.Module(ctx, owner, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, name))
	}

	return &v1.Module{
		Id:          mod.ID(),
		OwnerId:     mod.OwnerID(),
		Name:        mod.Name(),
		Description: mod.Description(),
		CreateTime:  timestamppb.New(mod.CreateTime()),
	}, nil
}

// ListModules lists modules for a specific owner (v1 API).
func (m *ModuleServiceV1) ListModules(ctx context.Context, req *connect.Request[v1.ListModulesRequest]) (*connect.Response[v1.ListModulesResponse], error) {
	if m.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "ListModulesV1", "ownerRefs", len(req.Msg.OwnerRefs))

	resp := connect.NewResponse(&v1.ListModulesResponse{})

	// If no owner refs, list all (not supported yet)
	if len(req.Msg.OwnerRefs) == 0 {
		return resp, nil
	}

	for _, ownerRef := range req.Msg.OwnerRefs {
		var ownerName string

		switch r := ownerRef.Value.(type) {
		case *ownerv1.OwnerRef_Id:
			// For ID lookup, we need to find owner by ID
			// Since we don't have OwnerByID, skip for now
			continue
		case *ownerv1.OwnerRef_Name:
			ownerName = r.Name
		default:
			continue
		}

		modules, err := m.svc.casReg.ListModules(ctx, ownerName)
		if err != nil {
			slog.ErrorContext(ctx, "failed to list modules", "owner", ownerName, "error", err)
			continue
		}

		for _, mod := range modules {
			resp.Msg.Modules = append(resp.Msg.Modules, &v1.Module{
				Id:          mod.ID(),
				OwnerId:     mod.OwnerID(),
				Name:        mod.Name(),
				Description: mod.Description(),
				CreateTime:  timestamppb.New(mod.CreateTime()),
			})
		}
	}

	return resp, nil
}

// CreateModules creates new modules (v1 API).
func (m *ModuleServiceV1) CreateModules(ctx context.Context, req *connect.Request[v1.CreateModulesRequest]) (*connect.Response[v1.CreateModulesResponse], error) {
	if m.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "CreateModulesV1", "values", len(req.Msg.Values))

	resp := connect.NewResponse(&v1.CreateModulesResponse{})

	for _, value := range req.Msg.Values {
		// Resolve owner
		var ownerName string
		switch r := value.OwnerRef.Value.(type) {
		case *ownerv1.OwnerRef_Id:
			// For ID lookup, we'd need OwnerByID - skip for now
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("owner lookup by ID not supported"))
		case *ownerv1.OwnerRef_Name:
			ownerName = r.Name
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid owner ref"))
		}

		description := value.Description

		mod, err := m.svc.casReg.CreateModule(ctx, ownerName, value.Name, description)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}

		resp.Msg.Modules = append(resp.Msg.Modules, &v1.Module{
			Id:          mod.ID(),
			OwnerId:     mod.OwnerID(),
			Name:        mod.Name(),
			Description: mod.Description(),
			CreateTime:  timestamppb.New(mod.CreateTime()),
		})
	}

	return resp, nil
}

// UpdateModules updates existing modules (v1 API).
func (m *ModuleServiceV1) UpdateModules(ctx context.Context, req *connect.Request[v1.UpdateModulesRequest]) (*connect.Response[v1.UpdateModulesResponse], error) {
	if m.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "UpdateModulesV1", "values", len(req.Msg.Values))

	resp := connect.NewResponse(&v1.UpdateModulesResponse{})

	for _, value := range req.Msg.Values {
		var mod *v1.Module
		var err error

		switch r := value.ModuleRef.Value.(type) {
		case *v1.ModuleRef_Id:
			mod, err = m.getModuleByID(ctx, r.Id)
		case *v1.ModuleRef_Name_:
			if r.Name != nil {
				mod, err = m.getModuleByName(ctx, r.Name.Owner, r.Name.Module)
			} else {
				err = errors.New("module name is nil")
			}
		default:
			err = errors.New("unknown module ref type")
		}

		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}

		// Note: UpdateModule not directly available on casReg
		// For now, return the module as-is (updates not persisted)
		resp.Msg.Modules = append(resp.Msg.Modules, mod)
	}

	return resp, nil
}

// DeleteModules deletes existing modules (v1 API).
func (m *ModuleServiceV1) DeleteModules(ctx context.Context, req *connect.Request[v1.DeleteModulesRequest]) (*connect.Response[v1.DeleteModulesResponse], error) {
	if m.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "DeleteModulesV1", "moduleRefs", len(req.Msg.ModuleRefs))

	resp := connect.NewResponse(&v1.DeleteModulesResponse{})

	for _, ref := range req.Msg.ModuleRefs {
		var owner, name string

		switch r := ref.Value.(type) {
		case *v1.ModuleRef_Id:
			mod, err := m.svc.casReg.ModuleByID(ctx, r.Id)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, err)
			}
			owner = mod.Owner()
			name = mod.Name()
		case *v1.ModuleRef_Name_:
			if r.Name != nil {
				owner = r.Name.Owner
				name = r.Name.Module
			} else {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module name is nil"))
			}
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown module ref type"))
		}

		if err := m.svc.casReg.DeleteModule(ctx, owner, name); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	return resp, nil
}
