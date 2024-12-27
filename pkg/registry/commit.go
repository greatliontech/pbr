package registry

import (
	"context"
	"encoding/hex"
	"fmt"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Get Commits.
func (reg *Registry) GetCommits(ctx context.Context, req *connect.Request[v1beta1.GetCommitsRequest]) (*connect.Response[v1beta1.GetCommitsResponse], error) {
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

		comt, err := reg.getCommit(ctx, m.Owner, m.Module, ref)
		if err != nil {
			return nil, err
		}

		resp.Msg.Commits = append(resp.Msg.Commits, comt)
	}

	return resp, nil
}

func (reg *Registry) getCommit(ctx context.Context, owner, modl, ref string) (*v1beta1.Commit, error) {
	ctx, span := tracer.Start(ctx, "getCommit", trace.WithAttributes(
		attribute.String("owner", owner),
		attribute.String("module", modl),
		attribute.String("ref", ref),
	))
	defer span.End()

	mod, err := reg.getModule(owner, modl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get module")
		return nil, err
	}
	_, mani, err := mod.FilesAndManifest(ref)
	if err != nil {
		return nil, err
	}
	comt, err := reg.getCommitObject(owner, modl, mani.Commit[:32], mani.SHAKE256)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get commit object")
		return nil, err
	}
	reg.commits[comt.Id] = comt
	reg.commitHashes[comt.Id] = mani.Commit
	reg.moduleIds[comt.ModuleId] = &internalModule{
		Owner:  owner,
		Module: modl,
	}
	reg.commitToModule[comt.Id] = reg.moduleIds[comt.ModuleId]

	return comt, nil
}

func (r *Registry) getCommitObject(owner, mod, id, dgst string) (*v1beta1.Commit, error) {
	fmt.Println("getCommit")
	digest, err := hex.DecodeString(dgst)
	if err != nil {
		return nil, err
	}
	ownerId := fakeUUID(owner)
	modId := fakeUUID(ownerId + "/" + mod)
	return &v1beta1.Commit{
		Id:       id,
		OwnerId:  ownerId,
		ModuleId: modId,
		Digest: &v1beta1.Digest{
			Type:  v1beta1.DigestType_DIGEST_TYPE_B4,
			Value: digest,
		},
	}, nil
}

// List Commits for a given Module, Label, or Commit.
func (reg *Registry) ListCommits(ctx context.Context, req *connect.Request[v1beta1.ListCommitsRequest]) (*connect.Response[v1beta1.ListCommitsResponse], error) {
	fmt.Println("ListCommits")
	return nil, nil
}
