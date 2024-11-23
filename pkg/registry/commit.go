package registry

import (
	"context"
	"fmt"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
)

// Get Commits.
func (reg *Registry) GetCommits(ctx context.Context, req *connect.Request[v1.GetCommitsRequest]) (*connect.Response[v1.GetCommitsResponse], error) {
	fmt.Println("GetCommits")
	return nil, nil
}

// List Commits for a given Module, Label, or Commit.
func (reg *Registry) ListCommits(ctx context.Context, req *connect.Request[v1.ListCommitsRequest]) (*connect.Response[v1.ListCommitsResponse], error) {
	fmt.Println("ListCommits")
	return nil, nil
}
