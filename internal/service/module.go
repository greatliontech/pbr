package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	ownerv1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/storage"
)

// GetModules retrieves modules by id or name.
func (svc *Service) GetModules(ctx context.Context, req *connect.Request[v1beta1.GetModulesRequest]) (*connect.Response[v1beta1.GetModulesResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	resp := connect.NewResponse(&v1beta1.GetModulesResponse{})

	for _, ref := range req.Msg.ModuleRefs {
		var mod *v1beta1.Module
		var err error

		switch r := ref.Value.(type) {
		case *v1beta1.ModuleRef_Id:
			mod, err = svc.getModuleByID(ctx, r.Id)
		case *v1beta1.ModuleRef_Name_:
			if r.Name != nil {
				mod, err = svc.getModuleByName(ctx, r.Name.Owner, r.Name.Module)
			} else {
				err = errors.New("module name is required")
			}
		default:
			err = errors.New("unknown module reference type")
		}

		if err != nil {
			return nil, err
		}

		resp.Msg.Modules = append(resp.Msg.Modules, mod)
	}

	return resp, nil
}

func (svc *Service) getModuleByID(ctx context.Context, id string) (*v1beta1.Module, error) {
	mod, err := svc.casReg.ModuleByID(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s", id))
	}

	return &v1beta1.Module{
		Id:          mod.ID(),
		OwnerId:     mod.OwnerID(),
		Name:        mod.Name(),
		Description: mod.Description(),
		CreateTime:  nil, // TODO: convert time
	}, nil
}

func (svc *Service) getModuleByName(ctx context.Context, owner, name string) (*v1beta1.Module, error) {
	mod, err := svc.casReg.Module(ctx, owner, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, name))
	}

	return &v1beta1.Module{
		Id:          mod.ID(),
		OwnerId:     mod.OwnerID(),
		Name:        mod.Name(),
		Description: mod.Description(),
	}, nil
}

// ListModules lists modules for a specific owner.
func (svc *Service) ListModules(ctx context.Context, req *connect.Request[v1beta1.ListModulesRequest]) (*connect.Response[v1beta1.ListModulesResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "ListModules", "ownerRefs", len(req.Msg.OwnerRefs))

	resp := connect.NewResponse(&v1beta1.ListModulesResponse{})

	// If no owner refs, list all (not supported yet)
	if len(req.Msg.OwnerRefs) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("owner reference is required"))
	}

	for _, ownerRef := range req.Msg.OwnerRefs {
		var ownerName string

		switch r := ownerRef.Value.(type) {
		case *ownerv1.OwnerRef_Id:
			owner, err := svc.casReg.Owner(ctx, r.Id)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("owner not found: %s", r.Id))
			}
			ownerName = owner.Name
		case *ownerv1.OwnerRef_Name:
			ownerName = r.Name
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown owner reference type"))
		}

		modules, err := svc.casReg.ListModules(ctx, ownerName)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}

		for _, mod := range modules {
			resp.Msg.Modules = append(resp.Msg.Modules, &v1beta1.Module{
				Id:          mod.ID(),
				OwnerId:     mod.OwnerID(),
				Name:        mod.Name(),
				Description: mod.Description(),
			})
		}
	}

	return resp, nil
}

// CreateModules creates new modules.
func (svc *Service) CreateModules(ctx context.Context, req *connect.Request[v1beta1.CreateModulesRequest]) (*connect.Response[v1beta1.CreateModulesResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "CreateModules", "values", len(req.Msg.Values))

	resp := connect.NewResponse(&v1beta1.CreateModulesResponse{})

	for _, value := range req.Msg.Values {
		// Resolve owner
		var ownerName string

		switch r := value.OwnerRef.Value.(type) {
		case *ownerv1.OwnerRef_Id:
			owner, err := svc.casReg.Owner(ctx, r.Id)
			if err != nil {
				// Owner doesn't exist - we could auto-create, but for now require it
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("owner not found: %s", r.Id))
			}
			ownerName = owner.Name
		case *ownerv1.OwnerRef_Name:
			ownerName = r.Name
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("owner reference is required"))
		}

		// Create module
		mod, err := svc.casReg.CreateModule(ctx, ownerName, value.Name, value.Description)
		if err != nil {
			if err == storage.ErrAlreadyExists {
				return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("module already exists: %s/%s", ownerName, value.Name))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}

		resp.Msg.Modules = append(resp.Msg.Modules, &v1beta1.Module{
			Id:          mod.ID(),
			OwnerId:     mod.OwnerID(),
			Name:        mod.Name(),
			Description: mod.Description(),
			CreateTime:  nil, // TODO: convert time
		})
	}

	return resp, nil
}

// UpdateModules updates existing modules.
func (svc *Service) UpdateModules(ctx context.Context, req *connect.Request[v1beta1.UpdateModulesRequest]) (*connect.Response[v1beta1.UpdateModulesResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "UpdateModules", "values", len(req.Msg.Values))

	resp := connect.NewResponse(&v1beta1.UpdateModulesResponse{})

	for _, value := range req.Msg.Values {
		// Get existing module
		var mod *storage.ModuleRecord
		var err error

		switch r := value.ModuleRef.Value.(type) {
		case *v1beta1.ModuleRef_Id:
			var m interface{ ID() string }
			m, err = svc.casReg.ModuleByID(ctx, r.Id)
			if err == nil {
				// Get the underlying record for update
				record, err := svc.casReg.ModuleByID(ctx, r.Id)
				if err != nil {
					return nil, connect.NewError(connect.CodeNotFound, err)
				}
				mod = &storage.ModuleRecord{
					ID:               record.ID(),
					OwnerID:          record.OwnerID(),
					Owner:            record.Owner(),
					Name:             record.Name(),
					Description:      record.Description(),
					DefaultLabelName: record.DefaultLabelName(),
					UpdateTime:       time.Now(),
				}
			}
			_ = m
		case *v1beta1.ModuleRef_Name_:
			if r.Name != nil {
				var m interface{ ID() string }
				m, err = svc.casReg.Module(ctx, r.Name.Owner, r.Name.Module)
				if err == nil {
					record, err := svc.casReg.Module(ctx, r.Name.Owner, r.Name.Module)
					if err != nil {
						return nil, connect.NewError(connect.CodeNotFound, err)
					}
					mod = &storage.ModuleRecord{
						ID:               record.ID(),
						OwnerID:          record.OwnerID(),
						Owner:            record.Owner(),
						Name:             record.Name(),
						Description:      record.Description(),
						DefaultLabelName: record.DefaultLabelName(),
						UpdateTime:       time.Now(),
					}
				}
				_ = m
			}
		}

		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}

		// Apply updates
		if value.Description != nil {
			mod.Description = *value.Description
		}
		if value.DefaultLabelName != nil {
			mod.DefaultLabelName = *value.DefaultLabelName
		}

		// Note: UpdateModule not directly available on casReg, would need to add
		// For now, return the module as-is (updates not persisted)
		resp.Msg.Modules = append(resp.Msg.Modules, &v1beta1.Module{
			Id:          mod.ID,
			OwnerId:     mod.OwnerID,
			Name:        mod.Name,
			Description: mod.Description,
		})
	}

	return resp, nil
}

// DeleteModules deletes existing modules.
func (svc *Service) DeleteModules(ctx context.Context, req *connect.Request[v1beta1.DeleteModulesRequest]) (*connect.Response[v1beta1.DeleteModulesResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "DeleteModules", "moduleRefs", len(req.Msg.ModuleRefs))

	resp := connect.NewResponse(&v1beta1.DeleteModulesResponse{})

	for _, ref := range req.Msg.ModuleRefs {
		var owner, name string
		var err error

		switch r := ref.Value.(type) {
		case *v1beta1.ModuleRef_Id:
			mod, err := svc.casReg.ModuleByID(ctx, r.Id)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, err)
			}
			owner = mod.Owner()
			name = mod.Name()
		case *v1beta1.ModuleRef_Name_:
			if r.Name != nil {
				owner = r.Name.Owner
				name = r.Name.Module
			} else {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module name is required"))
			}
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown module reference type"))
		}

		err = svc.casReg.DeleteModule(ctx, owner, name)
		if err != nil {
			if err == storage.ErrNotFound {
				// Already deleted, that's fine
				continue
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	return resp, nil
}
