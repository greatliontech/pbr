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

// CommitServiceV1 implements the v1 CommitService interface by wrapping Service.
type CommitServiceV1 struct {
	svc *Service
}

// NewCommitServiceV1 creates a new v1 CommitService wrapper.
func NewCommitServiceV1(svc *Service) *CommitServiceV1 {
	return &CommitServiceV1{svc: svc}
}

// GetCommits retrieves commits by resource references.
func (c *CommitServiceV1) GetCommits(
	ctx context.Context,
	req *connect.Request[v1.GetCommitsRequest],
) (*connect.Response[v1.GetCommitsResponse], error) {
	if c.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "GetCommitsV1", "resourceRefs", len(req.Msg.ResourceRefs))

	resp := &v1.GetCommitsResponse{
		Commits: make([]*v1.Commit, 0, len(req.Msg.ResourceRefs)),
	}

	for _, ref := range req.Msg.ResourceRefs {
		commit, err := c.getCommitByRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		resp.Commits = append(resp.Commits, commit)
	}

	return connect.NewResponse(resp), nil
}

// ListCommits lists commits for a given module, label, or commit.
func (c *CommitServiceV1) ListCommits(
	ctx context.Context,
	req *connect.Request[v1.ListCommitsRequest],
) (*connect.Response[v1.ListCommitsResponse], error) {
	if c.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "ListCommitsV1", "resourceRef", req.Msg.ResourceRef)

	// Parse resource ref to get module
	ref := req.Msg.ResourceRef
	if ref == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("resource_ref is required"))
	}

	var owner, modName string
	switch r := ref.Value.(type) {
	case *v1.ResourceRef_Name_:
		if r.Name == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
		}
		owner = r.Name.Owner
		modName = r.Name.Module
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only ResourceRef_Name is supported for ListCommits"))
	}

	mod, err := c.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// Get page size
	pageSize := int(req.Msg.PageSize)
	if pageSize <= 0 {
		pageSize = 100
	}

	commits, nextToken, err := mod.ListCommits(ctx, pageSize, req.Msg.PageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp := &v1.ListCommitsResponse{
		NextPageToken: nextToken,
	}

	for _, commit := range commits {
		protoCommit, err := c.commitToProto(commit)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.Commits = append(resp.Commits, protoCommit)
	}

	return connect.NewResponse(resp), nil
}

func (c *CommitServiceV1) getCommitByRef(ctx context.Context, ref *v1.ResourceRef) (*v1.Commit, error) {
	if ref == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("resource_ref is required"))
	}

	switch r := ref.Value.(type) {
	case *v1.ResourceRef_Id:
		return c.getCommitByID(ctx, r.Id)
	case *v1.ResourceRef_Name_:
		return c.getCommitByName(ctx, r.Name)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown resource ref type"))
	}
}

func (c *CommitServiceV1) getCommitByID(ctx context.Context, commitID string) (*v1.Commit, error) {
	slog.DebugContext(ctx, "getCommitByID v1", "commitID", commitID)

	mod, err := c.svc.casReg.ModuleByCommitID(ctx, commitID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	commit, err := mod.CommitByID(ctx, commitID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return c.commitToProto(commit)
}

func (c *CommitServiceV1) getCommitByName(ctx context.Context, name *v1.ResourceRef_Name) (*v1.Commit, error) {
	if name == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	owner := name.Owner
	modName := name.Module

	slog.DebugContext(ctx, "getCommitByName v1", "owner", owner, "module", modName)

	mod, err := c.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
	}

	// Determine the ref (label name or commit ref)
	var ref string
	switch child := name.Child.(type) {
	case *v1.ResourceRef_Name_LabelName:
		ref = child.LabelName
	case *v1.ResourceRef_Name_Ref:
		ref = child.Ref
	default:
		// No child specified - use default label (main)
		ref = "main"
	}

	commit, err := mod.Commit(ctx, ref)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref not found: %s", ref))
	}

	return c.commitToProto(commit)
}

func (c *CommitServiceV1) commitToProto(commit *cas.Commit) (*v1.Commit, error) {
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
