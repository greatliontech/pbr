package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry/cas"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ResourceServiceV1 implements the v1 ResourceService interface by wrapping Service.
type ResourceServiceV1 struct {
	svc *Service
}

// NewResourceServiceV1 creates a new v1 ResourceService wrapper.
func NewResourceServiceV1(svc *Service) *ResourceServiceV1 {
	return &ResourceServiceV1{svc: svc}
}

// GetResources resolves ResourceRefs to Resources.
func (r *ResourceServiceV1) GetResources(
	ctx context.Context,
	req *connect.Request[v1.GetResourcesRequest],
) (*connect.Response[v1.GetResourcesResponse], error) {
	if r.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "GetResourcesV1", "resourceRefs", len(req.Msg.ResourceRefs))

	resp := &v1.GetResourcesResponse{
		Resources: make([]*v1.Resource, 0, len(req.Msg.ResourceRefs)),
	}

	for _, ref := range req.Msg.ResourceRefs {
		resource, err := r.resolveResource(ctx, ref)
		if err != nil {
			return nil, err
		}
		resp.Resources = append(resp.Resources, resource)
	}

	return connect.NewResponse(resp), nil
}

func (r *ResourceServiceV1) resolveResource(ctx context.Context, ref *v1.ResourceRef) (*v1.Resource, error) {
	if ref == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("resource_ref is required"))
	}

	switch rv := ref.Value.(type) {
	case *v1.ResourceRef_Id:
		// ID could be a commit ID, module ID, or label ID
		// Try commit first
		return r.resolveByCommitID(ctx, rv.Id)
	case *v1.ResourceRef_Name_:
		return r.resolveByName(ctx, rv.Name)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown resource ref type"))
	}
}

func (r *ResourceServiceV1) resolveByCommitID(ctx context.Context, id string) (*v1.Resource, error) {
	slog.DebugContext(ctx, "resolveByCommitID v1", "id", id)

	mod, err := r.svc.casReg.ModuleByCommitID(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", id))
	}

	commit, err := mod.CommitByID(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	commitProto, err := r.commitToProto(commit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return &v1.Resource{
		Value: &v1.Resource_Commit{
			Commit: commitProto,
		},
	}, nil
}

func (r *ResourceServiceV1) resolveByName(ctx context.Context, name *v1.ResourceRef_Name) (*v1.Resource, error) {
	if name == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	owner := name.Owner
	modName := name.Module

	slog.DebugContext(ctx, "resolveByName v1", "owner", owner, "module", modName)

	mod, err := r.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
	}

	// Check what kind of resource is being requested
	switch child := name.Child.(type) {
	case *v1.ResourceRef_Name_LabelName:
		// Looking up a specific label
		commit, err := mod.Commit(ctx, child.LabelName)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("label not found: %s", child.LabelName))
		}
		labelProto := &v1.Label{
			Id:        mod.ID() + "-" + child.LabelName, // synthetic ID
			Name:      child.LabelName,
			OwnerId:   mod.OwnerID(),
			ModuleId:  mod.ID(),
			CommitId:  commit.ID,
			UpdatedByUserId: "",
		}
		return &v1.Resource{
			Value: &v1.Resource_Label{
				Label: labelProto,
			},
		}, nil

	case *v1.ResourceRef_Name_Ref:
		// Generic ref - could be commit ID or label name
		commit, err := mod.Commit(ctx, child.Ref)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref not found: %s", child.Ref))
		}
		commitProto, err := r.commitToProto(commit)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		return &v1.Resource{
			Value: &v1.Resource_Commit{
				Commit: commitProto,
			},
		}, nil

	default:
		// No child specified - return module
		moduleProto, err := r.moduleToProto(mod)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		return &v1.Resource{
			Value: &v1.Resource_Module{
				Module: moduleProto,
			},
		}, nil
	}
}

func (r *ResourceServiceV1) commitToProto(commit *cas.Commit) (*v1.Commit, error) {
	digest, err := hex.DecodeString(commit.ManifestDigest.Hex())
	if err != nil {
		return nil, fmt.Errorf("failed to decode digest: %w", err)
	}

	return &v1.Commit{
		Id:         commit.ID,
		OwnerId:    commit.OwnerID,
		ModuleId:   commit.ModuleID,
		CreateTime: timestamppb.New(commit.CreateTime),
		Digest: &v1.Digest{
			Type:  v1.DigestType_DIGEST_TYPE_B5,
			Value: digest,
		},
	}, nil
}

func (r *ResourceServiceV1) moduleToProto(mod *cas.Module) (*v1.Module, error) {
	return &v1.Module{
		Id:          mod.ID(),
		OwnerId:     mod.OwnerID(),
		Name:        mod.Name(),
		CreateTime:  timestamppb.New(mod.CreateTime()),
		Description: mod.Description(),
		State:       v1.ModuleState_MODULE_STATE_ACTIVE,
	}, nil
}
