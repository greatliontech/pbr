package service

import (
	"context"
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
	user := userFromContext(ctx)
	slog.DebugContext(ctx, "GetCommits", "user", user)

	resp := &connect.Response[v1beta1.GetCommitsResponse]{}
	resp.Msg = &v1beta1.GetCommitsResponse{}

	refs := []*v1beta1.ResourceRef_Name{}

	for _, val := range req.Msg.ResourceRefs {
		switch ref := val.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			return nil, fmt.Errorf("ResourceRef_Id not supported")
		case *v1beta1.ResourceRef_Name_:
			refs = append(refs, ref.Name)
		}
	}

	for _, m := range refs {
		ref := ""
		switch chld := m.Child.(type) {
		case *v1beta1.ResourceRef_Name_LabelName:
			ref = chld.LabelName
		case *v1beta1.ResourceRef_Name_Ref:
			ref = chld.Ref
		}

		comt, err := svc.getCommit(ctx, m.Owner, m.Module, ref)
		if err != nil {
			return nil, err
		}

		resp.Msg.Commits = append(resp.Msg.Commits, comt)
	}

	return resp, nil
}

func (svc *Service) getCommit(ctx context.Context, owner, modl, ref string) (*v1beta1.Commit, error) {
	ctx, span := tracer.Start(ctx, "service.getCommit", trace.WithAttributes(
		attribute.String("owner", owner),
		attribute.String("module", modl),
		attribute.String("ref", ref),
	))
	defer span.End()
	slog.DebugContext(ctx, "Service.getCommit", "owner", owner, "module", modl, "ref", ref)

	mod, err := svc.reg.Module(ctx, owner, modl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get module")
		return nil, err
	}

	c, err := mod.Commit(ctx, ref)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get internal commit object")
		return nil, err
	}

	comt, err := getCommitObject(c.OwnerId, c.ModuleId, c.CommitId, c.Digest)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to construct commit object")
		return nil, err
	}

	return comt, nil
}

// List Commits for a given Module, Label, or Commit.
func (svc *Service) ListCommits(ctx context.Context, req *connect.Request[v1beta1.ListCommitsRequest]) (*connect.Response[v1beta1.ListCommitsResponse], error) {
	fmt.Println("ListCommits")
	return nil, nil
}
