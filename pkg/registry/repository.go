package registry

import (
	"context"
	"fmt"
	"strings"

	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
)

func (reg *Registry) GetRepositoryByFullName(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositoryByFullNameRequest]) (*connect.Response[registryv1alpha1.GetRepositoryByFullNameResponse], error) {
	fmt.Println("GetRepositoryByFullName", req.Msg)
	parts := strings.Split(req.Msg.GetFullName(), "/")
	owner := parts[0]
	name := parts[1]
	r := &registryv1alpha1.Repository{
		Id:   req.Msg.GetFullName(),
		Name: name,
		Owner: &registryv1alpha1.Repository_OrganizationId{
			OrganizationId: owner,
		},
		OwnerName:     owner,
		Visibility:    registryv1alpha1.Visibility_VISIBILITY_PUBLIC,
		DefaultBranch: "main",
	}
	return &connect.Response[registryv1alpha1.GetRepositoryByFullNameResponse]{
		Msg: &registryv1alpha1.GetRepositoryByFullNameResponse{
			Repository: r,
		},
	}, nil
}

func (reg *Registry) GetRepositoriesByFullName(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositoriesByFullNameRequest]) (*connect.Response[registryv1alpha1.GetRepositoriesByFullNameResponse], error) {
	resp := &connect.Response[registryv1alpha1.GetRepositoriesByFullNameResponse]{
		Msg: &registryv1alpha1.GetRepositoriesByFullNameResponse{},
	}

	for _, repo := range req.Msg.FullNames {
		parts := strings.Split(repo, "/")
		owner := parts[0]
		name := parts[1]
		resp.Msg.Repositories = append(resp.Msg.Repositories, &registryv1alpha1.Repository{
			Id:   "ID" + repo,
			Name: name,
			Owner: &registryv1alpha1.Repository_OrganizationId{
				OrganizationId: owner,
			},
			OwnerName:     owner,
			Visibility:    registryv1alpha1.Visibility_VISIBILITY_PUBLIC,
			DefaultBranch: "main",
		})
	}

	return resp, nil
}
