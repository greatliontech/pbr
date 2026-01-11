package service

import (
	"context"
	"testing"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry/cas"
)

func TestGetOwners_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
	}

	ctx := context.Background()
	req := connect.NewRequest(&v1.GetOwnersRequest{})

	_, err := svc.GetOwners(ctx, req)
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

func TestGetOwners_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{},
	})

	resp, err := svc.GetOwners(ctx, req)
	if err != nil {
		t.Fatalf("GetOwners failed: %v", err)
	}

	if len(resp.Msg.Owners) != 0 {
		t.Errorf("expected 0 owners, got %d", len(resp.Msg.Owners))
	}
}

func TestGetOwners_ByName_NewOwner(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Name{
					Name: "newowner",
				},
			},
		},
	})

	resp, err := svc.GetOwners(ctx, req)
	if err != nil {
		t.Fatalf("GetOwners failed: %v", err)
	}

	if len(resp.Msg.Owners) != 1 {
		t.Fatalf("expected 1 owner, got %d", len(resp.Msg.Owners))
	}

	owner := resp.Msg.Owners[0]
	org, ok := owner.Value.(*v1.Owner_Organization)
	if !ok {
		t.Fatalf("expected organization, got %T", owner.Value)
	}

	if org.Organization.Name != "newowner" {
		t.Errorf("expected name 'newowner', got %q", org.Organization.Name)
	}
}

func TestGetOwners_ByName_ExistingOwner(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// First create a module to establish the owner
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "existingowner", "testmodule", files, []string{"main"})

	req := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Name{
					Name: "existingowner",
				},
			},
		},
	})

	resp, err := svc.GetOwners(ctx, req)
	if err != nil {
		t.Fatalf("GetOwners failed: %v", err)
	}

	if len(resp.Msg.Owners) != 1 {
		t.Fatalf("expected 1 owner, got %d", len(resp.Msg.Owners))
	}

	owner := resp.Msg.Owners[0]
	org, ok := owner.Value.(*v1.Owner_Organization)
	if !ok {
		t.Fatalf("expected organization, got %T", owner.Value)
	}

	if org.Organization.Name != "existingowner" {
		t.Errorf("expected name 'existingowner', got %q", org.Organization.Name)
	}

	// Should have an ID since the owner exists
	if org.Organization.Id == "" {
		t.Error("expected ID to be set for existing owner")
	}
}

func TestGetOwners_ById(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// First create a module to establish the owner
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	// Get the owner by name first to get the ID
	reqByName := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Name{
					Name: "testowner",
				},
			},
		},
	})

	respByName, err := svc.GetOwners(ctx, reqByName)
	if err != nil {
		t.Fatalf("GetOwners by name failed: %v", err)
	}

	ownerID := respByName.Msg.Owners[0].GetOrganization().Id

	// Now get by ID
	reqByID := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Id{
					Id: ownerID,
				},
			},
		},
	})

	respByID, err := svc.GetOwners(ctx, reqByID)
	if err != nil {
		t.Fatalf("GetOwners by ID failed: %v", err)
	}

	if len(respByID.Msg.Owners) != 1 {
		t.Fatalf("expected 1 owner, got %d", len(respByID.Msg.Owners))
	}

	owner := respByID.Msg.Owners[0]
	org, ok := owner.Value.(*v1.Owner_Organization)
	if !ok {
		t.Fatalf("expected organization, got %T", owner.Value)
	}

	if org.Organization.Id != ownerID {
		t.Errorf("expected ID %q, got %q", ownerID, org.Organization.Id)
	}
}

func TestGetOwners_ById_NotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Id{
					Id: "nonexistent-owner-id",
				},
			},
		},
	})

	_, err := svc.GetOwners(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent owner ID")
	}

	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

func TestGetOwners_MultipleOwners(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Create modules for different owners
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "owner1", "module1", files, []string{"main"})
	createTestModule(t, svc, "owner2", "module2", files, []string{"main"})

	req := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Name{
					Name: "owner1",
				},
			},
			{
				Value: &v1.OwnerRef_Name{
					Name: "owner2",
				},
			},
		},
	})

	resp, err := svc.GetOwners(ctx, req)
	if err != nil {
		t.Fatalf("GetOwners failed: %v", err)
	}

	if len(resp.Msg.Owners) != 2 {
		t.Errorf("expected 2 owners, got %d", len(resp.Msg.Owners))
	}

	// Verify both owners are returned
	names := make(map[string]bool)
	for _, owner := range resp.Msg.Owners {
		org := owner.GetOrganization()
		if org != nil {
			names[org.Name] = true
		}
	}

	if !names["owner1"] {
		t.Error("expected owner1 in response")
	}
	if !names["owner2"] {
		t.Error("expected owner2 in response")
	}
}

func TestGetOwners_MixedRefs(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a module to establish an owner
	files := []cas.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
	}
	createTestModule(t, svc, "existingowner", "testmodule", files, []string{"main"})

	// Get the owner ID
	reqByName := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Name{
					Name: "existingowner",
				},
			},
		},
	})

	respByName, err := svc.GetOwners(ctx, reqByName)
	if err != nil {
		t.Fatalf("GetOwners by name failed: %v", err)
	}

	ownerID := respByName.Msg.Owners[0].GetOrganization().Id

	// Now request with mixed refs: one by ID, one by name
	req := connect.NewRequest(&v1.GetOwnersRequest{
		OwnerRefs: []*v1.OwnerRef{
			{
				Value: &v1.OwnerRef_Id{
					Id: ownerID,
				},
			},
			{
				Value: &v1.OwnerRef_Name{
					Name: "virtualowner",
				},
			},
		},
	})

	resp, err := svc.GetOwners(ctx, req)
	if err != nil {
		t.Fatalf("GetOwners failed: %v", err)
	}

	if len(resp.Msg.Owners) != 2 {
		t.Errorf("expected 2 owners, got %d", len(resp.Msg.Owners))
	}
}
