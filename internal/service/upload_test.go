package service

import (
	"context"
	"testing"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
)

func TestUpload_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UploadRequest{})

	_, err := svc.Upload(ctx, req)
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

func TestUpload_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UploadRequest{
		Contents: []*v1beta1.UploadRequest_Content{},
	})

	resp, err := svc.Upload(ctx, req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if len(resp.Msg.Commits) != 0 {
		t.Errorf("expected 0 commits, got %d", len(resp.Msg.Commits))
	}
}

func TestUpload_SingleModule(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UploadRequest{
		Contents: []*v1beta1.UploadRequest_Content{
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Name_{
						Name: &v1beta1.ModuleRef_Name{
							Owner:  "testowner",
							Module: "testmodule",
						},
					},
				},
				Files: []*v1beta1.File{
					{
						Path:    "test.proto",
						Content: []byte("syntax = \"proto3\";\npackage test;"),
					},
					{
						Path:    "buf.yaml",
						Content: []byte("version: v1\nname: buf.build/testowner/testmodule"),
					},
				},
			},
		},
	})

	resp, err := svc.Upload(ctx, req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}

	commit := resp.Msg.Commits[0]
	if commit.Id == "" {
		t.Error("expected commit ID to be set")
	}
	if commit.ModuleId == "" {
		t.Error("expected module ID to be set")
	}
	if commit.OwnerId == "" {
		t.Error("expected owner ID to be set")
	}
}

func TestUpload_MultipleModules(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UploadRequest{
		Contents: []*v1beta1.UploadRequest_Content{
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Name_{
						Name: &v1beta1.ModuleRef_Name{
							Owner:  "testowner",
							Module: "module1",
						},
					},
				},
				Files: []*v1beta1.File{
					{
						Path:    "mod1.proto",
						Content: []byte("syntax = \"proto3\";\npackage mod1;"),
					},
				},
			},
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Name_{
						Name: &v1beta1.ModuleRef_Name{
							Owner:  "testowner",
							Module: "module2",
						},
					},
				},
				Files: []*v1beta1.File{
					{
						Path:    "mod2.proto",
						Content: []byte("syntax = \"proto3\";\npackage mod2;"),
					},
				},
			},
		},
	})

	resp, err := svc.Upload(ctx, req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if len(resp.Msg.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(resp.Msg.Commits))
	}
}

func TestUpload_WithLabels(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UploadRequest{
		Contents: []*v1beta1.UploadRequest_Content{
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Name_{
						Name: &v1beta1.ModuleRef_Name{
							Owner:  "testowner",
							Module: "testmodule",
						},
					},
				},
				Files: []*v1beta1.File{
					{
						Path:    "test.proto",
						Content: []byte("syntax = \"proto3\";\npackage test;"),
					},
				},
				ScopedLabelRefs: []*v1beta1.ScopedLabelRef{
					{
						Value: &v1beta1.ScopedLabelRef_Name{
							Name: "v1.0.0",
						},
					},
				},
			},
		},
	})

	resp, err := svc.Upload(ctx, req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}
}

func TestUpload_InvalidModuleRef(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Test with nil ModuleRef
	req := connect.NewRequest(&v1beta1.UploadRequest{
		Contents: []*v1beta1.UploadRequest_Content{
			{
				ModuleRef: nil,
				Files: []*v1beta1.File{
					{
						Path:    "test.proto",
						Content: []byte("syntax = \"proto3\";"),
					},
				},
			},
		},
	})

	_, err := svc.Upload(ctx, req)
	if err == nil {
		t.Fatal("expected error for nil module ref")
	}
}

func TestUpload_ModuleRefByID(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// First create a module
	mod, err := svc.casReg.GetOrCreateModule(ctx, "testowner", "testmodule")
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	// Now upload using the module ID
	req := connect.NewRequest(&v1beta1.UploadRequest{
		Contents: []*v1beta1.UploadRequest_Content{
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Id{
						Id: mod.ID(),
					},
				},
				Files: []*v1beta1.File{
					{
						Path:    "test.proto",
						Content: []byte("syntax = \"proto3\";\npackage test;"),
					},
				},
			},
		},
	})

	resp, err := svc.Upload(ctx, req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if len(resp.Msg.Commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(resp.Msg.Commits))
	}
}

func TestResolveModuleRef_NilRef(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	_, _, err := svc.resolveModuleRef(nil)
	if err == nil {
		t.Error("expected error for nil ref")
	}
}

func TestResolveModuleRef_ByName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ref := &v1beta1.ModuleRef{
		Value: &v1beta1.ModuleRef_Name_{
			Name: &v1beta1.ModuleRef_Name{
				Owner:  "myowner",
				Module: "mymodule",
			},
		},
	}

	owner, name, err := svc.resolveModuleRef(ref)
	if err != nil {
		t.Fatalf("resolveModuleRef failed: %v", err)
	}

	if owner != "myowner" {
		t.Errorf("expected owner 'myowner', got %q", owner)
	}
	if name != "mymodule" {
		t.Errorf("expected name 'mymodule', got %q", name)
	}
}

func TestResolveModuleRef_ByNameNilName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ref := &v1beta1.ModuleRef{
		Value: &v1beta1.ModuleRef_Name_{
			Name: nil,
		},
	}

	_, _, err := svc.resolveModuleRef(ref)
	if err == nil {
		t.Error("expected error for nil name")
	}
}

func TestExtractLabels(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	tests := []struct {
		name string
		refs []*v1beta1.ScopedLabelRef
		want []string
	}{
		{
			name: "empty refs",
			refs: nil,
			want: []string{},
		},
		{
			name: "single label",
			refs: []*v1beta1.ScopedLabelRef{
				{Value: &v1beta1.ScopedLabelRef_Name{Name: "main"}},
			},
			want: []string{"main"},
		},
		{
			name: "multiple labels",
			refs: []*v1beta1.ScopedLabelRef{
				{Value: &v1beta1.ScopedLabelRef_Name{Name: "main"}},
				{Value: &v1beta1.ScopedLabelRef_Name{Name: "v1.0.0"}},
			},
			want: []string{"main", "v1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.extractLabels(tt.refs)
			if len(got) != len(tt.want) {
				t.Errorf("extractLabels() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("extractLabels()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}
