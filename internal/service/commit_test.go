package service

import (
	"context"
	"testing"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry/cas"
)

func TestGetCommits_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
		tokens: map[string]string{"testtoken": "testuser"},
	}

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{})

	_, err := svc.GetCommits(ctx, req)
	if err == nil {
		t.Fatal("expected error when CAS not configured")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeUnimplemented {
		t.Errorf("expected CodeUnimplemented, got %v", connectErr.Code())
	}
}

func TestGetCommits_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{},
	})

	resp, err := svc.GetCommits(ctx, req)
	if err != nil {
		t.Fatalf("GetCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 0 {
		t.Errorf("expected 0 commits, got %d", len(resp.Msg.Commits))
	}
}

func TestGetCommits_ByID(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with a commit
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	commit := createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{
			{
				Value: &v1beta1.ResourceRef_Id{
					Id: commit.ID,
				},
			},
		},
	})

	resp, err := svc.GetCommits(ctx, req)
	if err != nil {
		t.Fatalf("GetCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}

	if resp.Msg.Commits[0].Id != commit.ID {
		t.Errorf("expected commit ID %q, got %q", commit.ID, resp.Msg.Commits[0].Id)
	}
}

func TestGetCommits_ByName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with a commit
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{
			{
				Value: &v1beta1.ResourceRef_Name_{
					Name: &v1beta1.ResourceRef_Name{
						Owner:  "testowner",
						Module: "testmodule",
						Child: &v1beta1.ResourceRef_Name_LabelName{
							LabelName: "main",
						},
					},
				},
			},
		},
	})

	resp, err := svc.GetCommits(ctx, req)
	if err != nil {
		t.Fatalf("GetCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}
}

func TestGetCommits_ByNameWithRef(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with a commit and multiple labels
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main", "v1.0.0"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{
			{
				Value: &v1beta1.ResourceRef_Name_{
					Name: &v1beta1.ResourceRef_Name{
						Owner:  "testowner",
						Module: "testmodule",
						Child: &v1beta1.ResourceRef_Name_Ref{
							Ref: "v1.0.0",
						},
					},
				},
			},
		},
	})

	resp, err := svc.GetCommits(ctx, req)
	if err != nil {
		t.Fatalf("GetCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}
}

func TestGetCommits_CommitNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{
			{
				Value: &v1beta1.ResourceRef_Id{
					Id: "nonexistent-commit-id",
				},
			},
		},
	})

	_, err := svc.GetCommits(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent commit")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

func TestGetCommits_ModuleNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{
			{
				Value: &v1beta1.ResourceRef_Name_{
					Name: &v1beta1.ResourceRef_Name{
						Owner:  "nonexistent",
						Module: "nonexistent",
						Child: &v1beta1.ResourceRef_Name_LabelName{
							LabelName: "main",
						},
					},
				},
			},
		},
	})

	_, err := svc.GetCommits(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent module")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

func TestGetCommits_MultipleCommits(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create two modules
	files1 := []cas.File{
		{Path: "mod1.proto", Content: "syntax = \"proto3\";\npackage mod1;"},
	}
	commit1 := createTestModule(t, svc, "testowner", "module1", files1, []string{"main"})

	files2 := []cas.File{
		{Path: "mod2.proto", Content: "syntax = \"proto3\";\npackage mod2;"},
	}
	commit2 := createTestModule(t, svc, "testowner", "module2", files2, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetCommitsRequest{
		ResourceRefs: []*v1beta1.ResourceRef{
			{
				Value: &v1beta1.ResourceRef_Id{
					Id: commit1.ID,
				},
			},
			{
				Value: &v1beta1.ResourceRef_Id{
					Id: commit2.ID,
				},
			},
		},
	})

	resp, err := svc.GetCommits(ctx, req)
	if err != nil {
		t.Fatalf("GetCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(resp.Msg.Commits))
	}
}

func TestListCommits_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListCommitsRequest{})

	_, err := svc.ListCommits(ctx, req)
	if err == nil {
		t.Fatal("expected error when CAS not configured")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeUnimplemented {
		t.Errorf("expected CodeUnimplemented, got %v", connectErr.Code())
	}
}

func TestListCommits_InvalidResourceRef(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListCommitsRequest{
		ResourceRef: &v1beta1.ResourceRef{
			Value: &v1beta1.ResourceRef_Id{
				Id: "someid",
			},
		},
	})

	_, err := svc.ListCommits(ctx, req)
	if err == nil {
		t.Fatal("expected error for ResourceRef_Id")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}

func TestListCommits_ModuleNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListCommitsRequest{
		ResourceRef: &v1beta1.ResourceRef{
			Value: &v1beta1.ResourceRef_Name_{
				Name: &v1beta1.ResourceRef_Name{
					Owner:  "nonexistent",
					Module: "nonexistent",
				},
			},
		},
	})

	_, err := svc.ListCommits(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent module")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

func TestListCommits_Success(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with commits
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListCommitsRequest{
		ResourceRef: &v1beta1.ResourceRef{
			Value: &v1beta1.ResourceRef_Name_{
				Name: &v1beta1.ResourceRef_Name{
					Owner:  "testowner",
					Module: "testmodule",
				},
			},
		},
	})

	resp, err := svc.ListCommits(ctx, req)
	if err != nil {
		t.Fatalf("ListCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}
}

func TestListCommits_WithPagination(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module
	ctx := context.Background()
	mod, err := svc.casReg.GetOrCreateModule(ctx, "testowner", "testmodule")
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	// Create multiple commits
	for i := 0; i < 5; i++ {
		files := []cas.File{
			{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test" + string(rune('a'+i)) + ";"},
		}
		_, err := mod.CreateCommit(ctx, files, []string{"main"}, "", nil)
		if err != nil {
			t.Fatalf("failed to create commit: %v", err)
		}
	}

	// List with page size 2
	req := connect.NewRequest(&v1beta1.ListCommitsRequest{
		ResourceRef: &v1beta1.ResourceRef{
			Value: &v1beta1.ResourceRef_Name_{
				Name: &v1beta1.ResourceRef_Name{
					Owner:  "testowner",
					Module: "testmodule",
				},
			},
		},
		PageSize: 2,
	})

	resp, err := svc.ListCommits(ctx, req)
	if err != nil {
		t.Fatalf("ListCommits failed: %v", err)
	}

	if len(resp.Msg.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(resp.Msg.Commits))
	}

	// Should have next page token if there are more commits
	// (This depends on implementation - adjust as needed)
}

func TestListCommits_DefaultPageSize(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListCommitsRequest{
		ResourceRef: &v1beta1.ResourceRef{
			Value: &v1beta1.ResourceRef_Name_{
				Name: &v1beta1.ResourceRef_Name{
					Owner:  "testowner",
					Module: "testmodule",
				},
			},
		},
		PageSize: 0, // Should use default
	})

	resp, err := svc.ListCommits(ctx, req)
	if err != nil {
		t.Fatalf("ListCommits failed: %v", err)
	}

	// Should succeed with default page size
	if resp.Msg.Commits == nil {
		t.Error("expected non-nil commits slice")
	}
}
