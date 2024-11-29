package registry

import (
	"context"
	"encoding/hex"
	"fmt"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
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
		repo, err := reg.getRepository(ctx, m.Owner, m.Module)
		if err != nil {
			fmt.Println("GetCommits error", err)
			return nil, err
		}
		ref := ""
		switch chld := m.Child.(type) {
		case *v1beta1.ResourceRef_Name_LabelName:
			ref = chld.LabelName
		case *v1beta1.ResourceRef_Name_Ref:
			ref = chld.Ref
		}
		_, mani, err := repo.FilesAndManifest(ref)
		if err != nil {
			return nil, err
		}
		maniDigest, err := hex.DecodeString(mani.SHAKE256)
		if err != nil {
			return nil, err
		}
		comt := &v1beta1.Commit{
			Id:       mani.Commit[:32],
			OwnerId:  fakeUUID(m.Owner),
			ModuleId: fakeUUID(m.Owner + "/" + m.Module),
			Digest: &v1beta1.Digest{
				Type:  v1beta1.DigestType_DIGEST_TYPE_B4,
				Value: maniDigest,
			},
		}
		reg.commits[comt.Id] = comt
		resp.Msg.Commits = append(resp.Msg.Commits, comt)
	}

	return resp, nil
}

// List Commits for a given Module, Label, or Commit.
func (reg *Registry) ListCommits(ctx context.Context, req *connect.Request[v1beta1.ListCommitsRequest]) (*connect.Response[v1beta1.ListCommitsResponse], error) {
	fmt.Println("ListCommits")
	return nil, nil
}
