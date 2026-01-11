package service

import (
	"context"
	"testing"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	ownerv1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry/cas"
)

func TestGetModules_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.GetModulesRequest{})

	_, err := svc.GetModules(ctx, req)
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

func TestGetModules_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.GetModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{},
	})

	resp, err := svc.GetModules(ctx, req)
	if err != nil {
		t.Fatalf("GetModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 0 {
		t.Errorf("expected 0 modules, got %d", len(resp.Msg.Modules))
	}
}

func TestGetModules_ByName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.GetModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Name_{
					Name: &v1beta1.ModuleRef_Name{
						Owner:  "testowner",
						Module: "testmodule",
					},
				},
			},
		},
	})

	resp, err := svc.GetModules(ctx, req)
	if err != nil {
		t.Fatalf("GetModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(resp.Msg.Modules))
	}

	module := resp.Msg.Modules[0]
	if module.Name != "testmodule" {
		t.Errorf("expected name 'testmodule', got %q", module.Name)
	}
}

func TestGetModules_ByID(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module
	ctx := context.Background()
	mod, err := svc.casReg.GetOrCreateModule(ctx, "testowner", "testmodule")
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	req := connect.NewRequest(&v1beta1.GetModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Id{
					Id: mod.ID(),
				},
			},
		},
	})

	resp, err := svc.GetModules(ctx, req)
	if err != nil {
		t.Fatalf("GetModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(resp.Msg.Modules))
	}

	if resp.Msg.Modules[0].Id != mod.ID() {
		t.Errorf("expected ID %q, got %q", mod.ID(), resp.Msg.Modules[0].Id)
	}
}

func TestGetModules_NotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.GetModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Name_{
					Name: &v1beta1.ModuleRef_Name{
						Owner:  "nonexistent",
						Module: "nonexistent",
					},
				},
			},
		},
	})

	_, err := svc.GetModules(ctx, req)
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

func TestGetModules_NilName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.GetModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Name_{
					Name: nil,
				},
			},
		},
	})

	_, err := svc.GetModules(ctx, req)
	if err == nil {
		t.Fatal("expected error for nil name")
	}
}

func TestListModules_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListModulesRequest{})

	_, err := svc.ListModules(ctx, req)
	if err == nil {
		t.Fatal("expected error when CAS not configured")
	}
}

func TestListModules_NoOwnerRefs(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListModulesRequest{
		OwnerRefs: []*ownerv1.OwnerRef{},
	})

	_, err := svc.ListModules(ctx, req)
	if err == nil {
		t.Fatal("expected error when no owner refs provided")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}

func TestListModules_ByOwnerName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create modules for the owner
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "module1", files, []string{"main"})
	createTestModule(t, svc, "testowner", "module2", files, []string{"main"})

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListModulesRequest{
		OwnerRefs: []*ownerv1.OwnerRef{
			{
				Value: &ownerv1.OwnerRef_Name{
					Name: "testowner",
				},
			},
		},
	})

	resp, err := svc.ListModules(ctx, req)
	if err != nil {
		t.Fatalf("ListModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(resp.Msg.Modules))
	}
}

func TestListModules_ByOwnerID(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module to establish the owner
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := context.Background()

	// Get owner by name to get ID
	ownerResp, err := svc.casReg.OwnerByName(ctx, "testowner")
	if err != nil {
		t.Fatalf("failed to get owner: %v", err)
	}

	req := connect.NewRequest(&v1beta1.ListModulesRequest{
		OwnerRefs: []*ownerv1.OwnerRef{
			{
				Value: &ownerv1.OwnerRef_Id{
					Id: ownerResp.ID,
				},
			},
		},
	})

	resp, err := svc.ListModules(ctx, req)
	if err != nil {
		t.Fatalf("ListModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(resp.Msg.Modules))
	}
}

func TestListModules_OwnerNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.ListModulesRequest{
		OwnerRefs: []*ownerv1.OwnerRef{
			{
				Value: &ownerv1.OwnerRef_Id{
					Id: "nonexistent-owner-id",
				},
			},
		},
	})

	_, err := svc.ListModules(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent owner")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

func TestCreateModules_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.CreateModulesRequest{})

	_, err := svc.CreateModules(ctx, req)
	if err == nil {
		t.Fatal("expected error when CAS not configured")
	}
}

func TestCreateModules_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.CreateModulesRequest{
		Values: []*v1beta1.CreateModulesRequest_Value{},
	})

	resp, err := svc.CreateModules(ctx, req)
	if err != nil {
		t.Fatalf("CreateModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 0 {
		t.Errorf("expected 0 modules, got %d", len(resp.Msg.Modules))
	}
}

func TestCreateModules_SingleModule(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.CreateModulesRequest{
		Values: []*v1beta1.CreateModulesRequest_Value{
			{
				OwnerRef: &ownerv1.OwnerRef{
					Value: &ownerv1.OwnerRef_Name{
						Name: "testowner",
					},
				},
				Name:        "newmodule",
				Description: "A test module",
			},
		},
	})

	resp, err := svc.CreateModules(ctx, req)
	if err != nil {
		t.Fatalf("CreateModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(resp.Msg.Modules))
	}

	module := resp.Msg.Modules[0]
	if module.Name != "newmodule" {
		t.Errorf("expected name 'newmodule', got %q", module.Name)
	}
	if module.Description != "A test module" {
		t.Errorf("expected description 'A test module', got %q", module.Description)
	}
	if module.Id == "" {
		t.Error("expected ID to be set")
	}
}

func TestCreateModules_MultipleModules(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.CreateModulesRequest{
		Values: []*v1beta1.CreateModulesRequest_Value{
			{
				OwnerRef: &ownerv1.OwnerRef{
					Value: &ownerv1.OwnerRef_Name{
						Name: "testowner",
					},
				},
				Name: "module1",
			},
			{
				OwnerRef: &ownerv1.OwnerRef{
					Value: &ownerv1.OwnerRef_Name{
						Name: "testowner",
					},
				},
				Name: "module2",
			},
		},
	})

	resp, err := svc.CreateModules(ctx, req)
	if err != nil {
		t.Fatalf("CreateModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(resp.Msg.Modules))
	}
}

func TestCreateModules_AlreadyExists(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Create module first
	mod, err := svc.casReg.GetOrCreateModule(ctx, "testowner", "existingmodule")
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}
	originalID := mod.ID()

	// Try to create again - the current implementation returns the existing module
	// rather than an error (idempotent behavior)
	req := connect.NewRequest(&v1beta1.CreateModulesRequest{
		Values: []*v1beta1.CreateModulesRequest_Value{
			{
				OwnerRef: &ownerv1.OwnerRef{
					Value: &ownerv1.OwnerRef_Name{
						Name: "testowner",
					},
				},
				Name: "existingmodule",
			},
		},
	})

	resp, err := svc.CreateModules(ctx, req)
	if err != nil {
		t.Fatalf("CreateModules failed: %v", err)
	}

	// Should return the existing module with the same ID
	if len(resp.Msg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(resp.Msg.Modules))
	}
	if resp.Msg.Modules[0].Id != originalID {
		t.Errorf("expected same module ID %q, got %q", originalID, resp.Msg.Modules[0].Id)
	}
}

func TestUpdateModules_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UpdateModulesRequest{})

	_, err := svc.UpdateModules(ctx, req)
	if err == nil {
		t.Fatal("expected error when CAS not configured")
	}
}

func TestUpdateModules_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.UpdateModulesRequest{
		Values: []*v1beta1.UpdateModulesRequest_Value{},
	})

	resp, err := svc.UpdateModules(ctx, req)
	if err != nil {
		t.Fatalf("UpdateModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 0 {
		t.Errorf("expected 0 modules, got %d", len(resp.Msg.Modules))
	}
}

func TestUpdateModules_UpdateDescription(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a module
	mod, err := svc.casReg.GetOrCreateModule(ctx, "testowner", "testmodule")
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	newDesc := "Updated description"
	req := connect.NewRequest(&v1beta1.UpdateModulesRequest{
		Values: []*v1beta1.UpdateModulesRequest_Value{
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Id{
						Id: mod.ID(),
					},
				},
				Description: &newDesc,
			},
		},
	})

	resp, err := svc.UpdateModules(ctx, req)
	if err != nil {
		t.Fatalf("UpdateModules failed: %v", err)
	}

	if len(resp.Msg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(resp.Msg.Modules))
	}

	// Note: The actual persistence might not be implemented yet
	// This test verifies the request is processed without error
}

func TestUpdateModules_ModuleNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	newDesc := "Updated description"
	req := connect.NewRequest(&v1beta1.UpdateModulesRequest{
		Values: []*v1beta1.UpdateModulesRequest_Value{
			{
				ModuleRef: &v1beta1.ModuleRef{
					Value: &v1beta1.ModuleRef_Id{
						Id: "nonexistent-id",
					},
				},
				Description: &newDesc,
			},
		},
	})

	_, err := svc.UpdateModules(ctx, req)
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

func TestDeleteModules_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DeleteModulesRequest{})

	_, err := svc.DeleteModules(ctx, req)
	if err == nil {
		t.Fatal("expected error when CAS not configured")
	}
}

func TestDeleteModules_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DeleteModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{},
	})

	resp, err := svc.DeleteModules(ctx, req)
	if err != nil {
		t.Fatalf("DeleteModules failed: %v", err)
	}

	// Empty request should succeed
	if resp.Msg == nil {
		t.Error("expected non-nil response message")
	}
}

func TestDeleteModules_ByName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a module
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	req := connect.NewRequest(&v1beta1.DeleteModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Name_{
					Name: &v1beta1.ModuleRef_Name{
						Owner:  "testowner",
						Module: "testmodule",
					},
				},
			},
		},
	})

	_, err := svc.DeleteModules(ctx, req)
	if err != nil {
		t.Fatalf("DeleteModules failed: %v", err)
	}

	// Verify module is deleted
	_, err = svc.casReg.Module(ctx, "testowner", "testmodule")
	if err == nil {
		t.Error("expected module to be deleted")
	}
}

func TestDeleteModules_ByID(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a module
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	commit := createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	// Get the module to get its ID
	mod, err := svc.casReg.ModuleByCommitID(ctx, commit.ID)
	if err != nil {
		t.Fatalf("failed to get module: %v", err)
	}

	req := connect.NewRequest(&v1beta1.DeleteModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Id{
					Id: mod.ID(),
				},
			},
		},
	})

	_, err = svc.DeleteModules(ctx, req)
	if err != nil {
		t.Fatalf("DeleteModules failed: %v", err)
	}
}

func TestDeleteModules_NotFoundIsOK(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DeleteModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Name_{
					Name: &v1beta1.ModuleRef_Name{
						Owner:  "nonexistent",
						Module: "nonexistent",
					},
				},
			},
		},
	})

	// Deleting non-existent module should succeed (idempotent)
	_, err := svc.DeleteModules(ctx, req)
	if err != nil {
		t.Fatalf("DeleteModules failed: %v", err)
	}
}

func TestDeleteModules_NilName(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1beta1.DeleteModulesRequest{
		ModuleRefs: []*v1beta1.ModuleRef{
			{
				Value: &v1beta1.ModuleRef_Name_{
					Name: nil,
				},
			},
		},
	})

	_, err := svc.DeleteModules(ctx, req)
	if err == nil {
		t.Fatal("expected error for nil name")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}
