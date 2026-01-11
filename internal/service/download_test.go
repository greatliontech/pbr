package service

import (
	"context"
	"testing"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry/cas"
)

func TestDownload_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{})

	_, err := svc.Download(ctx, req)
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

func TestDownload_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{
		Values: []*v1beta1.DownloadRequest_Value{},
	})

	resp, err := svc.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(resp.Msg.Contents) != 0 {
		t.Errorf("expected 0 contents, got %d", len(resp.Msg.Contents))
	}
}

func TestDownload_CommitNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{
		Values: []*v1beta1.DownloadRequest_Value{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: "nonexistent-commit-id",
					},
				},
			},
		},
	})

	_, err := svc.Download(ctx, req)
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

func TestDownload_SingleModule(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with files
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/testmodule"},
	}
	commit := createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{
		Values: []*v1beta1.DownloadRequest_Value{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: commit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(resp.Msg.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(resp.Msg.Contents))
	}

	content := resp.Msg.Contents[0]

	// Verify commit
	if content.Commit == nil {
		t.Fatal("expected commit in content")
	}
	if content.Commit.Id != commit.ID {
		t.Errorf("expected commit ID %q, got %q", commit.ID, content.Commit.Id)
	}

	// Verify files
	if len(content.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(content.Files))
	}

	// Verify buf.yaml was detected
	if content.V1BufYamlFile == nil {
		t.Error("expected V1BufYamlFile to be set")
	}
}

func TestDownload_WithBufLock(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with buf.lock
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/testmodule"},
		{Path: "buf.lock", Content: "version: v1\ndeps: []"},
	}
	commit := createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{
		Values: []*v1beta1.DownloadRequest_Value{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: commit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	content := resp.Msg.Contents[0]

	// Verify buf.lock was detected
	if content.V1BufLockFile == nil {
		t.Error("expected V1BufLockFile to be set")
	}
	if content.V1BufLockFile.Path != "buf.lock" {
		t.Errorf("expected buf.lock path, got %q", content.V1BufLockFile.Path)
	}
}

func TestDownload_MultipleModules(t *testing.T) {
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

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{
		Values: []*v1beta1.DownloadRequest_Value{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: commit1.ID,
					},
				},
			},
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: commit2.ID,
					},
				},
			},
		},
	})

	resp, err := svc.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(resp.Msg.Contents) != 2 {
		t.Errorf("expected 2 contents, got %d", len(resp.Msg.Contents))
	}
}

func TestDownload_ResourceRefNameNotSupported(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DownloadRequest{
		Values: []*v1beta1.DownloadRequest_Value{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Name_{
						Name: &v1beta1.ResourceRef_Name{
							Owner:  "testowner",
							Module: "testmodule",
						},
					},
				},
			},
		},
	})

	_, err := svc.Download(ctx, req)
	if err == nil {
		t.Fatal("expected error for ResourceRef_Name")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}

func TestBuildDownloadContent(t *testing.T) {
	commit := &v1beta1.Commit{
		Id:       "testcommit",
		ModuleId: "testmodule",
		OwnerId:  "testowner",
	}

	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";"},
		{Path: "buf.yaml", Content: "version: v1"},
		{Path: "buf.lock", Content: "version: v1\ndeps: []"},
		{Path: "other.txt", Content: "some content"},
	}

	content := buildDownloadContent(commit, files)

	// Verify commit
	if content.Commit.Id != commit.Id {
		t.Errorf("expected commit ID %q, got %q", commit.Id, content.Commit.Id)
	}

	// Verify files count
	if len(content.Files) != 4 {
		t.Errorf("expected 4 files, got %d", len(content.Files))
	}

	// Verify buf.yaml detection
	if content.V1BufYamlFile == nil {
		t.Error("expected V1BufYamlFile to be set")
	}
	if string(content.V1BufYamlFile.Content) != "version: v1" {
		t.Errorf("unexpected buf.yaml content: %q", string(content.V1BufYamlFile.Content))
	}

	// Verify buf.lock detection
	if content.V1BufLockFile == nil {
		t.Error("expected V1BufLockFile to be set")
	}
	if string(content.V1BufLockFile.Content) != "version: v1\ndeps: []" {
		t.Errorf("unexpected buf.lock content: %q", string(content.V1BufLockFile.Content))
	}
}

func TestBuildDownloadContent_NoBufYamlOrLock(t *testing.T) {
	commit := &v1beta1.Commit{
		Id:       "testcommit",
		ModuleId: "testmodule",
		OwnerId:  "testowner",
	}

	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";"},
		{Path: "other.proto", Content: "syntax = \"proto3\";"},
	}

	content := buildDownloadContent(commit, files)

	if content.V1BufYamlFile != nil {
		t.Error("expected V1BufYamlFile to be nil")
	}
	if content.V1BufLockFile != nil {
		t.Error("expected V1BufLockFile to be nil")
	}
}
