package registry

import (
	"context"
	"fmt"
	"strings"

	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func (reg *Registry) GetRepository(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositoryRequest]) (*connect.Response[registryv1alpha1.GetRepositoryResponse], error) {
	return &connect.Response[registryv1alpha1.GetRepositoryResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) ListRepositories(ctx context.Context, req *connect.Request[registryv1alpha1.ListRepositoriesRequest]) (*connect.Response[registryv1alpha1.ListRepositoriesResponse], error) {
	return &connect.Response[registryv1alpha1.ListRepositoriesResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) ListUserRepositories(ctx context.Context, req *connect.Request[registryv1alpha1.ListUserRepositoriesRequest]) (*connect.Response[registryv1alpha1.ListUserRepositoriesResponse], error) {
	return &connect.Response[registryv1alpha1.ListUserRepositoriesResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) ListRepositoriesUserCanAccess(ctx context.Context, req *connect.Request[registryv1alpha1.ListRepositoriesUserCanAccessRequest]) (*connect.Response[registryv1alpha1.ListRepositoriesUserCanAccessResponse], error) {
	return &connect.Response[registryv1alpha1.ListRepositoriesUserCanAccessResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) ListOrganizationRepositories(ctx context.Context, req *connect.Request[registryv1alpha1.ListOrganizationRepositoriesRequest]) (*connect.Response[registryv1alpha1.ListOrganizationRepositoriesResponse], error) {
	return &connect.Response[registryv1alpha1.ListOrganizationRepositoriesResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) CreateRepositoryByFullName(ctx context.Context, req *connect.Request[registryv1alpha1.CreateRepositoryByFullNameRequest]) (*connect.Response[registryv1alpha1.CreateRepositoryByFullNameResponse], error) {
	return &connect.Response[registryv1alpha1.CreateRepositoryByFullNameResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) DeleteRepository(ctx context.Context, req *connect.Request[registryv1alpha1.DeleteRepositoryRequest]) (*connect.Response[registryv1alpha1.DeleteRepositoryResponse], error) {
	return &connect.Response[registryv1alpha1.DeleteRepositoryResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) DeleteRepositoryByFullName(ctx context.Context, req *connect.Request[registryv1alpha1.DeleteRepositoryByFullNameRequest]) (*connect.Response[registryv1alpha1.DeleteRepositoryByFullNameResponse], error) {
	return &connect.Response[registryv1alpha1.DeleteRepositoryByFullNameResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) DeprecateRepositoryByName(ctx context.Context, req *connect.Request[registryv1alpha1.DeprecateRepositoryByNameRequest]) (*connect.Response[registryv1alpha1.DeprecateRepositoryByNameResponse], error) {
	return &connect.Response[registryv1alpha1.DeprecateRepositoryByNameResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) UndeprecateRepositoryByName(ctx context.Context, req *connect.Request[registryv1alpha1.UndeprecateRepositoryByNameRequest]) (*connect.Response[registryv1alpha1.UndeprecateRepositoryByNameResponse], error) {
	return &connect.Response[registryv1alpha1.UndeprecateRepositoryByNameResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) SetRepositoryContributor(ctx context.Context, req *connect.Request[registryv1alpha1.SetRepositoryContributorRequest]) (*connect.Response[registryv1alpha1.SetRepositoryContributorResponse], error) {
	return &connect.Response[registryv1alpha1.SetRepositoryContributorResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) ListRepositoryContributors(ctx context.Context, req *connect.Request[registryv1alpha1.ListRepositoryContributorsRequest]) (*connect.Response[registryv1alpha1.ListRepositoryContributorsResponse], error) {
	return &connect.Response[registryv1alpha1.ListRepositoryContributorsResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetRepositoryContributor(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositoryContributorRequest]) (*connect.Response[registryv1alpha1.GetRepositoryContributorResponse], error) {
	return &connect.Response[registryv1alpha1.GetRepositoryContributorResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetRepositorySettings(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositorySettingsRequest]) (*connect.Response[registryv1alpha1.GetRepositorySettingsResponse], error) {
	return &connect.Response[registryv1alpha1.GetRepositorySettingsResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) UpdateRepositorySettingsByName(ctx context.Context, req *connect.Request[registryv1alpha1.UpdateRepositorySettingsByNameRequest]) (*connect.Response[registryv1alpha1.UpdateRepositorySettingsByNameResponse], error) {
	return &connect.Response[registryv1alpha1.UpdateRepositorySettingsByNameResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetRepositoriesMetadata(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositoriesMetadataRequest]) (*connect.Response[registryv1alpha1.GetRepositoriesMetadataResponse], error) {
	return &connect.Response[registryv1alpha1.GetRepositoriesMetadataResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetRepositoryDependencyDOTString(ctx context.Context, req *connect.Request[registryv1alpha1.GetRepositoryDependencyDOTStringRequest]) (*connect.Response[registryv1alpha1.GetRepositoryDependencyDOTStringResponse], error) {
	return &connect.Response[registryv1alpha1.GetRepositoryDependencyDOTStringResponse]{}, status.Error(codes.Unimplemented, "not implemented")
}
