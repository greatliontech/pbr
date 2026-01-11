package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Get Commits.
func (svc *Service) GetCommits(ctx context.Context, req *connect.Request[v1beta1.GetCommitsRequest]) (*connect.Response[v1beta1.GetCommitsResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	user := userFromContext(ctx)
	slog.DebugContext(ctx, "GetCommits", "user", user)

	resp := &connect.Response[v1beta1.GetCommitsResponse]{}
	resp.Msg = &v1beta1.GetCommitsResponse{}

	for _, val := range req.Msg.ResourceRefs {
		var comt *v1beta1.Commit
		var err error

		switch ref := val.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			comt, err = svc.getCommitByID(ctx, ref.Id)
		case *v1beta1.ResourceRef_Name_:
			labelOrRef := ""
			switch chld := ref.Name.Child.(type) {
			case *v1beta1.ResourceRef_Name_LabelName:
				labelOrRef = chld.LabelName
			case *v1beta1.ResourceRef_Name_Ref:
				labelOrRef = chld.Ref
			}
			comt, err = svc.getCommit(ctx, ref.Name.Owner, ref.Name.Module, labelOrRef)
		}

		if err != nil {
			return nil, err
		}

		resp.Msg.Commits = append(resp.Msg.Commits, comt)
	}

	return resp, nil
}

func (svc *Service) getCommitByID(ctx context.Context, commitID string) (*v1beta1.Commit, error) {
	ctx, span := tracer.Start(ctx, "service.getCommitByID", trace.WithAttributes(
		attribute.String("commitID", commitID),
	))
	defer span.End()
	slog.DebugContext(ctx, "Service.getCommitByID", "commitID", commitID)

	mod, err := svc.casReg.ModuleByCommitID(ctx, commitID)
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

	return getCommitObject(commit.OwnerID, commit.ModuleID, commit.ID, commit.ManifestDigest.Hex())
}

func (svc *Service) getCommit(ctx context.Context, owner, modl, ref string) (*v1beta1.Commit, error) {
	ctx, span := tracer.Start(ctx, "service.getCommit", trace.WithAttributes(
		attribute.String("owner", owner),
		attribute.String("module", modl),
		attribute.String("ref", ref),
	))
	defer span.End()
	slog.DebugContext(ctx, "Service.getCommit", "owner", owner, "module", modl, "ref", ref)

	mod, err := svc.casReg.Module(ctx, owner, modl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get module")
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modl))
	}

	commit, err := mod.Commit(ctx, ref)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get commit")
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found for ref: %s", ref))
	}

	comt, err := getCommitObject(commit.OwnerID, commit.ModuleID, commit.ID, commit.ManifestDigest.Hex())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to construct commit object")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return comt, nil
}

// List Commits for a given Module, Label, or Commit.
func (svc *Service) ListCommits(ctx context.Context, req *connect.Request[v1beta1.ListCommitsRequest]) (*connect.Response[v1beta1.ListCommitsResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "ListCommits", "resourceRef", req.Msg.ResourceRef)

	// Parse resource ref to get module
	var owner, modl string
	switch ref := req.Msg.ResourceRef.Value.(type) {
	case *v1beta1.ResourceRef_Name_:
		owner = ref.Name.Owner
		modl = ref.Name.Module
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only ResourceRef_Name is supported for ListCommits"))
	}

	mod, err := svc.casReg.Module(ctx, owner, modl)
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

	resp := &v1beta1.ListCommitsResponse{
		NextPageToken: nextToken,
	}

	for _, commit := range commits {
		c, err := getCommitObject(commit.OwnerID, commit.ModuleID, commit.ID, commit.ManifestDigest.Hex())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.Commits = append(resp.Commits, c)
	}

	return connect.NewResponse(resp), nil
}
