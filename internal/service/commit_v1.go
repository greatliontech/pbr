package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// CommitServiceV1 implements the v1 CommitService interface by wrapping Service.
type CommitServiceV1 struct {
	svc *Service
}

// NewCommitServiceV1 creates a new v1 CommitService wrapper.
func NewCommitServiceV1(svc *Service) *CommitServiceV1 {
	return &CommitServiceV1{svc: svc}
}

// GetCommits retrieves commits by resource reference.
// This v1 endpoint returns commits with B5 digests (instead of B4 in v1beta1).
func (c *CommitServiceV1) GetCommits(ctx context.Context, req *connect.Request[v1.GetCommitsRequest]) (*connect.Response[v1.GetCommitsResponse], error) {
	if c.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	user := userFromContext(ctx)
	slog.DebugContext(ctx, "GetCommitsV1", "user", user)

	resp := &connect.Response[v1.GetCommitsResponse]{}
	resp.Msg = &v1.GetCommitsResponse{}

	for _, ref := range req.Msg.ResourceRefs {
		var commit *v1.Commit
		var err error

		switch r := ref.Value.(type) {
		case *v1.ResourceRef_Id:
			commit, err = c.getCommitByIDV1(ctx, r.Id)
		case *v1.ResourceRef_Name_:
			labelOrRef := ""
			switch child := r.Name.Child.(type) {
			case *v1.ResourceRef_Name_LabelName:
				labelOrRef = child.LabelName
			case *v1.ResourceRef_Name_Ref:
				labelOrRef = child.Ref
			}
			commit, err = c.getCommitV1(ctx, r.Name.Owner, r.Name.Module, labelOrRef)
		}

		if err != nil {
			return nil, err
		}

		resp.Msg.Commits = append(resp.Msg.Commits, commit)
	}

	return resp, nil
}

func (c *CommitServiceV1) getCommitByIDV1(ctx context.Context, commitID string) (*v1.Commit, error) {
	ctx, span := tracer.Start(ctx, "service.getCommitByIDV1", trace.WithAttributes(
		attribute.String("commitID", commitID),
	))
	defer span.End()
	slog.DebugContext(ctx, "CommitServiceV1.getCommitByIDV1", "commitID", commitID)

	mod, err := c.svc.casReg.ModuleByCommitID(ctx, commitID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get module by commit ID")
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	commit, err := mod.CommitByID(ctx, commitID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get commit by ID")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return getCommitObjectV1(commit), nil
}

func (c *CommitServiceV1) getCommitV1(ctx context.Context, owner, modl, ref string) (*v1.Commit, error) {
	ctx, span := tracer.Start(ctx, "service.getCommitV1", trace.WithAttributes(
		attribute.String("owner", owner),
		attribute.String("module", modl),
		attribute.String("ref", ref),
	))
	defer span.End()
	slog.DebugContext(ctx, "CommitServiceV1.getCommitV1", "owner", owner, "module", modl, "ref", ref)

	mod, err := c.svc.casReg.Module(ctx, owner, modl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get module")
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modl))
	}

	// If no ref specified, default to main
	if ref == "" {
		ref = "main"
	}

	commit, err := mod.Commit(ctx, ref)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get commit")
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found for ref: %s", ref))
	}

	return getCommitObjectV1(commit), nil
}

// ListCommits lists commits for a given module, label, or commit.
// This v1 endpoint returns commits with B5 digests (instead of B4 in v1beta1).
func (c *CommitServiceV1) ListCommits(ctx context.Context, req *connect.Request[v1.ListCommitsRequest]) (*connect.Response[v1.ListCommitsResponse], error) {
	if c.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "ListCommitsV1", "resourceRef", req.Msg.ResourceRef)

	// Parse resource ref to get module
	var owner, modl string
	switch ref := req.Msg.ResourceRef.Value.(type) {
	case *v1.ResourceRef_Name_:
		owner = ref.Name.Owner
		modl = ref.Name.Module
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only ResourceRef_Name is supported for ListCommits"))
	}

	mod, err := c.svc.casReg.Module(ctx, owner, modl)
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
		resp.Commits = append(resp.Commits, getCommitObjectV1(commit))
	}

	return connect.NewResponse(resp), nil
}
