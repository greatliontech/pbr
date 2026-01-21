package service

import (
	"context"
	"testing"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/storage"
	"gocloud.dev/blob/memblob"
	"gocloud.dev/docstore/memdocstore"
)

func setupTestService(t *testing.T) (*Service, func()) {
	t.Helper()

	bucket := memblob.OpenBucket(nil)

	blobStore := storage.NewBlobStore(bucket)
	manifestStore := storage.NewManifestStore(bucket)

	owners, _ := memdocstore.OpenCollection("ID", nil)
	modules, _ := memdocstore.OpenCollection("ID", nil)
	commits, _ := memdocstore.OpenCollection("ID", nil)
	labels, _ := memdocstore.OpenCollection("ID", nil)
	metadataStore := storage.NewMetadataStore(owners, modules, commits, labels)

	casReg := registry.New(blobStore, manifestStore, metadataStore, "test.registry.com")

	svc := &Service{
		conf: &config.Config{
			Host: "test.registry.com",
		},
		casReg: casReg,
		tokens: map[string]string{"testtoken": "testuser"},
		users:  map[string]string{"testuser": "testtoken"},
	}

	cleanup := func() {
		bucket.Close()
		owners.Close()
		modules.Close()
		commits.Close()
		labels.Close()
	}

	return svc, cleanup
}

func createTestModule(t *testing.T, svc *Service, owner, name string, files []registry.File, labels []string) *registry.Commit {
	t.Helper()
	return createTestModuleWithDeps(t, svc, owner, name, files, labels, nil)
}

func createTestModuleWithDeps(t *testing.T, svc *Service, owner, name string, files []registry.File, labels []string, depCommitIDs []string) *registry.Commit {
	t.Helper()

	ctx := context.Background()
	mod, err := svc.casReg.GetOrCreateModule(ctx, owner, name)
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	commit, err := mod.CreateCommit(ctx, files, labels, "", depCommitIDs)
	if err != nil {
		t.Fatalf("failed to create commit: %v", err)
	}

	return commit
}

func TestGetGraph_NoCASConfigured(t *testing.T) {
	svc := &Service{
		conf:   &config.Config{Host: "test.registry.com"},
		casReg: nil,
		tokens: map[string]string{"testtoken": "testuser"},
	}

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{})

	_, err := svc.GetGraph(ctx, req)
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

func TestGetGraph_CommitNotFound(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: "nonexistent-commit-id",
					},
				},
			},
		},
	})

	_, err := svc.GetGraph(ctx, req)
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

func TestGetGraph_SingleModuleNoDependencies(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with no buf.lock (no dependencies)
	files := []registry.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/testmodule"},
	}
	commit := createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: commit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	if resp.Msg.Graph == nil {
		t.Fatal("expected graph in response")
	}

	// Should have exactly 1 commit (the requested one)
	if len(resp.Msg.Graph.Commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have no edges (no dependencies)
	if len(resp.Msg.Graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(resp.Msg.Graph.Edges))
	}

	// Verify the commit details
	if resp.Msg.Graph.Commits[0].Commit.Id != commit.ID {
		t.Errorf("expected commit ID %s, got %s", commit.ID, resp.Msg.Graph.Commits[0].Commit.Id)
	}
	if resp.Msg.Graph.Commits[0].Registry != "test.registry.com" {
		t.Errorf("expected registry test.registry.com, got %s", resp.Msg.Graph.Commits[0].Registry)
	}
}

func TestGetGraph_SingleDependency(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create dependency module first
	depFiles := []registry.File{
		{Path: "dep.proto", Content: "syntax = \"proto3\";\npackage dep;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/depmodule"},
	}
	depCommit := createTestModule(t, svc, "testowner", "depmodule", depFiles, []string{"main"})

	// Create main module with dependency (pass depCommitIDs)
	mainFiles := []registry.File{
		{Path: "main.proto", Content: "syntax = \"proto3\";\npackage main;\nimport \"dep.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/mainmodule"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: test.registry.com
    owner: testowner
    repository: depmodule
    commit: ` + depCommit.ID + `
    digest: shake256:` + depCommit.ManifestDigest.Hex()},
	}
	mainCommit := createTestModuleWithDeps(t, svc, "testowner", "mainmodule", mainFiles, []string{"main"}, []string{depCommit.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: mainCommit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 2 commits (main + dependency)
	if len(resp.Msg.Graph.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have 1 edge (main -> dep)
	if len(resp.Msg.Graph.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(resp.Msg.Graph.Edges))
	}

	// Verify the edge
	edge := resp.Msg.Graph.Edges[0]
	if edge.FromNode.CommitId != mainCommit.ID {
		t.Errorf("expected from commit %s, got %s", mainCommit.ID, edge.FromNode.CommitId)
	}
	if edge.ToNode.CommitId != depCommit.ID {
		t.Errorf("expected to commit %s, got %s", depCommit.ID, edge.ToNode.CommitId)
	}
}

func TestGetGraph_ChainedDependencies(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create C (no deps)
	cFiles := []registry.File{
		{Path: "c.proto", Content: "syntax = \"proto3\";\npackage c;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/modulec"},
	}
	cCommit := createTestModule(t, svc, "testowner", "modulec", cFiles, []string{"main"})

	// Create B (depends on C)
	bFiles := []registry.File{
		{Path: "b.proto", Content: "syntax = \"proto3\";\npackage b;\nimport \"c.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/moduleb"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: test.registry.com
    owner: testowner
    repository: modulec
    commit: ` + cCommit.ID + `
    digest: shake256:` + cCommit.ManifestDigest.Hex()},
	}
	bCommit := createTestModuleWithDeps(t, svc, "testowner", "moduleb", bFiles, []string{"main"}, []string{cCommit.ID})

	// Create A (depends on B)
	aFiles := []registry.File{
		{Path: "a.proto", Content: "syntax = \"proto3\";\npackage a;\nimport \"b.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/modulea"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: test.registry.com
    owner: testowner
    repository: moduleb
    commit: ` + bCommit.ID + `
    digest: shake256:` + bCommit.ManifestDigest.Hex()},
	}
	aCommit := createTestModuleWithDeps(t, svc, "testowner", "modulea", aFiles, []string{"main"}, []string{bCommit.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: aCommit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 3 commits (A, B, C)
	if len(resp.Msg.Graph.Commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have 2 edges (A -> B, B -> C)
	if len(resp.Msg.Graph.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(resp.Msg.Graph.Edges))
	}

	// Verify all edges exist
	edgeMap := make(map[string]string)
	for _, e := range resp.Msg.Graph.Edges {
		edgeMap[e.FromNode.CommitId] = e.ToNode.CommitId
	}

	if edgeMap[aCommit.ID] != bCommit.ID {
		t.Errorf("expected edge A -> B")
	}
	if edgeMap[bCommit.ID] != cCommit.ID {
		t.Errorf("expected edge B -> C")
	}
}

// TestGetGraph_DiamondWithDifferentVersions_NewerProcessedLast tests the scenario where:
// - mid-a depends on base@v1 (older)
// - mid-b depends on base@v2 (newer)
// - top depends on [mid-a, mid-b] (mid-a processed first)
//
// The graph service should resolve to base@v2 (newer) regardless of processing order.
func TestGetGraph_DiamondWithDifferentVersions_NewerProcessedLast(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base@v1
	baseV1Files := []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage BaseMessage { string id = 1; }"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}
	baseV1Commit := createTestModule(t, svc, "testowner", "base", baseV1Files, []string{"v1"})

	// Create mid-a (depends on base@v1)
	midAFiles := []registry.File{
		{Path: "mida.proto", Content: "syntax = \"proto3\";\npackage mida;\nimport \"base.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/mida"},
	}
	midACommit := createTestModuleWithDeps(t, svc, "testowner", "mida", midAFiles, []string{"main"}, []string{baseV1Commit.ID})

	// Create base@v2 (new version with additional field)
	baseV2Files := []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage BaseMessage { string id = 1; string name = 2; }"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}
	baseV2Commit := createTestModuleWithDeps(t, svc, "testowner", "base", baseV2Files, []string{"v2", "main"}, nil)

	// Verify base@v1 and base@v2 have different commit IDs
	if baseV1Commit.ID == baseV2Commit.ID {
		t.Fatalf("expected different commit IDs for base v1 and v2, got same: %s", baseV1Commit.ID)
	}

	// Create mid-b (depends on base@v2)
	midBFiles := []registry.File{
		{Path: "midb.proto", Content: "syntax = \"proto3\";\npackage midb;\nimport \"base.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/midb"},
	}
	midBCommit := createTestModuleWithDeps(t, svc, "testowner", "midb", midBFiles, []string{"main"}, []string{baseV2Commit.ID})

	// Create top (depends on mid-a and mid-b)
	topFiles := []registry.File{
		{Path: "top.proto", Content: "syntax = \"proto3\";\npackage top;\nimport \"mida.proto\";\nimport \"midb.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/top"},
	}
	topCommit := createTestModuleWithDeps(t, svc, "testowner", "top", topFiles, []string{"main"}, []string{midACommit.ID, midBCommit.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: topCommit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 4 commits: top, mid-a, mid-b, base (deduplicated to v2)
	if len(resp.Msg.Graph.Commits) != 4 {
		t.Errorf("expected 4 commits (top, mid-a, mid-b, base), got %d", len(resp.Msg.Graph.Commits))
		for _, c := range resp.Msg.Graph.Commits {
			t.Logf("  commit: %s", c.Commit.Id)
		}
	}

	// Should have 4 edges: top->mid-a, top->mid-b, mid-a->base@v2, mid-b->base@v2
	// Note: both mid-a and mid-b edges point to base@v2 due to "last seen wins"
	if len(resp.Msg.Graph.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(resp.Msg.Graph.Edges))
		for _, e := range resp.Msg.Graph.Edges {
			t.Logf("  edge: %s -> %s", e.FromNode.CommitId, e.ToNode.CommitId)
		}
	}

	// Build edge map for verification
	edgeMap := make(map[string][]string)
	for _, e := range resp.Msg.Graph.Edges {
		edgeMap[e.FromNode.CommitId] = append(edgeMap[e.FromNode.CommitId], e.ToNode.CommitId)
	}

	// Verify top -> mid-a and top -> mid-b
	topEdges := edgeMap[topCommit.ID]
	if len(topEdges) != 2 {
		t.Errorf("expected 2 edges from top, got %d", len(topEdges))
	}

	// Verify mid-a -> base@v2 (updated due to "last seen wins")
	midAEdges := edgeMap[midACommit.ID]
	if len(midAEdges) != 1 || midAEdges[0] != baseV2Commit.ID {
		t.Errorf("expected mid-a -> base@v2 (due to resolution), got %v", midAEdges)
	}

	// Verify mid-b -> base@v2
	midBEdges := edgeMap[midBCommit.ID]
	if len(midBEdges) != 1 || midBEdges[0] != baseV2Commit.ID {
		t.Errorf("expected mid-b -> base@v2, got %v", midBEdges)
	}

	// Verify only base@v2 is in the commits (v1 was replaced)
	commitIDs := make(map[string]bool)
	for _, c := range resp.Msg.Graph.Commits {
		commitIDs[c.Commit.Id] = true
	}
	if commitIDs[baseV1Commit.ID] {
		t.Error("base@v1 should not be in commits (should be replaced by v2)")
	}
	if !commitIDs[baseV2Commit.ID] {
		t.Error("base@v2 not found in commits")
	}
}

// TestGetGraph_DiamondWithDifferentVersions_NewerProcessedFirst tests the scenario where:
// - mid-a depends on base@v2 (newer)
// - mid-b depends on base@v1 (older)
// - top depends on [mid-a, mid-b] (mid-a processed first, so newer version seen first)
//
// The graph service should STILL resolve to base@v2 (newer) even though the older
// version is processed last. This ensures we keep the newer version, not "last seen".
func TestGetGraph_DiamondWithDifferentVersions_NewerProcessedFirst(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base@v1 (older)
	baseV1Files := []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage BaseMessage { string id = 1; }"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}
	baseV1Commit := createTestModule(t, svc, "testowner", "base", baseV1Files, []string{"v1"})

	// Create base@v2 (newer) - created AFTER v1, so it has a later timestamp
	baseV2Files := []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage BaseMessage { string id = 1; string name = 2; }"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}
	baseV2Commit := createTestModuleWithDeps(t, svc, "testowner", "base", baseV2Files, []string{"v2", "main"}, nil)

	// Verify base@v1 and base@v2 have different commit IDs
	if baseV1Commit.ID == baseV2Commit.ID {
		t.Fatalf("expected different commit IDs for base v1 and v2, got same: %s", baseV1Commit.ID)
	}

	// Create mid-a (depends on base@v2 - the NEWER version)
	midAFiles := []registry.File{
		{Path: "mida.proto", Content: "syntax = \"proto3\";\npackage mida;\nimport \"base.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/mida"},
	}
	midACommit := createTestModuleWithDeps(t, svc, "testowner", "mida", midAFiles, []string{"main"}, []string{baseV2Commit.ID})

	// Create mid-b (depends on base@v1 - the OLDER version)
	midBFiles := []registry.File{
		{Path: "midb.proto", Content: "syntax = \"proto3\";\npackage midb;\nimport \"base.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/midb"},
	}
	midBCommit := createTestModuleWithDeps(t, svc, "testowner", "midb", midBFiles, []string{"main"}, []string{baseV1Commit.ID})

	// Create top (depends on mid-a THEN mid-b)
	// mid-a is processed first (depends on v2), mid-b processed second (depends on v1)
	// We should KEEP v2 because it's newer, not switch to v1 just because it's "last seen"
	topFiles := []registry.File{
		{Path: "top.proto", Content: "syntax = \"proto3\";\npackage top;\nimport \"mida.proto\";\nimport \"midb.proto\";"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/top"},
	}
	topCommit := createTestModuleWithDeps(t, svc, "testowner", "top", topFiles, []string{"main"}, []string{midACommit.ID, midBCommit.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: topCommit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 4 commits: top, mid-a, mid-b, base (deduplicated to v2 - the NEWER one)
	if len(resp.Msg.Graph.Commits) != 4 {
		t.Errorf("expected 4 commits (top, mid-a, mid-b, base), got %d", len(resp.Msg.Graph.Commits))
		for _, c := range resp.Msg.Graph.Commits {
			t.Logf("  commit: %s", c.Commit.Id)
		}
	}

	// Should have 4 edges: top->mid-a, top->mid-b, mid-a->base@v2, mid-b->base@v2
	if len(resp.Msg.Graph.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(resp.Msg.Graph.Edges))
		for _, e := range resp.Msg.Graph.Edges {
			t.Logf("  edge: %s -> %s", e.FromNode.CommitId, e.ToNode.CommitId)
		}
	}

	// Build edge map for verification
	edgeMap := make(map[string][]string)
	for _, e := range resp.Msg.Graph.Edges {
		edgeMap[e.FromNode.CommitId] = append(edgeMap[e.FromNode.CommitId], e.ToNode.CommitId)
	}

	// Verify mid-a -> base@v2 (the version it originally depended on)
	midAEdges := edgeMap[midACommit.ID]
	if len(midAEdges) != 1 || midAEdges[0] != baseV2Commit.ID {
		t.Errorf("expected mid-a -> base@v2, got %v", midAEdges)
	}

	// Verify mid-b -> base@v2 (updated to newer version, NOT its original v1)
	midBEdges := edgeMap[midBCommit.ID]
	if len(midBEdges) != 1 || midBEdges[0] != baseV2Commit.ID {
		t.Errorf("expected mid-b -> base@v2 (resolved to newer), got %v", midBEdges)
		t.Logf("base@v1 commit: %s", baseV1Commit.ID)
		t.Logf("base@v2 commit: %s", baseV2Commit.ID)
	}

	// Verify only base@v2 is in the commits (v1 should NOT be there)
	commitIDs := make(map[string]bool)
	for _, c := range resp.Msg.Graph.Commits {
		commitIDs[c.Commit.Id] = true
	}
	if commitIDs[baseV1Commit.ID] {
		t.Error("base@v1 should not be in commits (should keep v2 as it's newer)")
	}
	if !commitIDs[baseV2Commit.ID] {
		t.Error("base@v2 not found in commits")
	}
}

func TestGetGraph_DiamondDependencies(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Diamond pattern: A -> B, A -> C, B -> D, C -> D
	// D (base, no deps)
	dFiles := []registry.File{
		{Path: "d.proto", Content: "syntax = \"proto3\";\npackage d;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/moduled"},
	}
	dCommit := createTestModule(t, svc, "testowner", "moduled", dFiles, []string{"main"})

	// B (depends on D)
	bFiles := []registry.File{
		{Path: "b.proto", Content: "syntax = \"proto3\";\npackage b;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/moduleb"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: test.registry.com
    owner: testowner
    repository: moduled
    commit: ` + dCommit.ID + `
    digest: shake256:` + dCommit.ManifestDigest.Hex()},
	}
	bCommit := createTestModuleWithDeps(t, svc, "testowner", "moduleb", bFiles, []string{"main"}, []string{dCommit.ID})

	// C (depends on D)
	cFiles := []registry.File{
		{Path: "c.proto", Content: "syntax = \"proto3\";\npackage c;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/modulec"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: test.registry.com
    owner: testowner
    repository: moduled
    commit: ` + dCommit.ID + `
    digest: shake256:` + dCommit.ManifestDigest.Hex()},
	}
	cCommit := createTestModuleWithDeps(t, svc, "testowner", "modulec", cFiles, []string{"main"}, []string{dCommit.ID})

	// A (depends on B and C)
	aFiles := []registry.File{
		{Path: "a.proto", Content: "syntax = \"proto3\";\npackage a;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/modulea"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: test.registry.com
    owner: testowner
    repository: moduleb
    commit: ` + bCommit.ID + `
    digest: shake256:` + bCommit.ManifestDigest.Hex() + `
  - remote: test.registry.com
    owner: testowner
    repository: modulec
    commit: ` + cCommit.ID + `
    digest: shake256:` + cCommit.ManifestDigest.Hex()},
	}
	aCommit := createTestModuleWithDeps(t, svc, "testowner", "modulea", aFiles, []string{"main"}, []string{bCommit.ID, cCommit.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: aCommit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 4 commits (A, B, C, D)
	if len(resp.Msg.Graph.Commits) != 4 {
		t.Errorf("expected 4 commits, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have 4 edges (A->B, A->C, B->D, C->D)
	if len(resp.Msg.Graph.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(resp.Msg.Graph.Edges))
	}

	// D should appear only once in commits (deduplication)
	commitIDs := make(map[string]int)
	for _, c := range resp.Msg.Graph.Commits {
		commitIDs[c.Commit.Id]++
	}
	if commitIDs[dCommit.ID] != 1 {
		t.Errorf("expected D to appear once, got %d", commitIDs[dCommit.ID])
	}
}

func TestGetGraph_ExternalDependency(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create module with external dependency (different registry)
	// Note: External dependencies are not tracked via DepCommitIDs since they're
	// not in our registry. The buf CLI resolves external deps directly from their
	// registries. Our graph only tracks local dependencies via stored DepCommitIDs.
	mainFiles := []registry.File{
		{Path: "main.proto", Content: "syntax = \"proto3\";\npackage main;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/mainmodule"},
		{Path: "buf.lock", Content: `version: v1
deps:
  - remote: buf.build
    owner: googleapis
    repository: googleapis
    commit: cc916c31859748a68fd229a3c8d7a2e8
    digest: shake256:469b049d0f58c6eedc4f3ae52e5b4395a99d6417e0d5a3cdd04b400dc4b3e4f41d7ce326a96c1d1c955a7fdf61a3e6b0c31a3da9692d5d72e4e50a30a9e16f10`},
	}
	mainCommit := createTestModule(t, svc, "testowner", "mainmodule", mainFiles, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Id{
						Id: mainCommit.ID,
					},
				},
			},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 1 commit (main only - external deps are not tracked via DepCommitIDs)
	if len(resp.Msg.Graph.Commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have 0 edges (external deps are resolved by buf CLI from their registries)
	if len(resp.Msg.Graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(resp.Msg.Graph.Edges))
	}
}

func TestGetGraph_MultipleRootModules(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create two independent modules
	files1 := []registry.File{
		{Path: "mod1.proto", Content: "syntax = \"proto3\";\npackage mod1;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/module1"},
	}
	commit1 := createTestModule(t, svc, "testowner", "module1", files1, []string{"main"})

	files2 := []registry.File{
		{Path: "mod2.proto", Content: "syntax = \"proto3\";\npackage mod2;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/module2"},
	}
	commit2 := createTestModule(t, svc, "testowner", "module2", files2, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
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

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 2 commits
	if len(resp.Msg.Graph.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have 0 edges (independent modules)
	if len(resp.Msg.Graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(resp.Msg.Graph.Edges))
	}
}

func TestGetGraph_ResourceRefNameSupported(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module first
	files := []registry.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/testmodule"},
	}
	commit := createTestModule(t, svc, "testowner", "testmodule", files, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
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

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 1 commit
	if len(resp.Msg.Graph.Commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(resp.Msg.Graph.Commits))
	}

	// Verify the commit ID matches
	if resp.Msg.Graph.Commits[0].Commit.Id != commit.ID {
		t.Errorf("expected commit ID %s, got %s", commit.ID, resp.Msg.Graph.Commits[0].Commit.Id)
	}
}

func TestGetGraph_EmptyRequest(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should return empty graph
	if len(resp.Msg.Graph.Commits) != 0 {
		t.Errorf("expected 0 commits, got %d", len(resp.Msg.Graph.Commits))
	}
	if len(resp.Msg.Graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(resp.Msg.Graph.Edges))
	}
}

// TestGetGraph_DeepDependencyChain tests a deep chain: A -> B -> C -> D -> E -> F
func TestGetGraph_DeepDependencyChain(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create F (no deps)
	fCommit := createTestModule(t, svc, "testowner", "f", []registry.File{
		{Path: "f.proto", Content: "syntax = \"proto3\";\npackage f;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/f"},
	}, []string{"main"})

	// Create E -> F
	eCommit := createTestModuleWithDeps(t, svc, "testowner", "e", []registry.File{
		{Path: "e.proto", Content: "syntax = \"proto3\";\npackage e;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/e"},
	}, []string{"main"}, []string{fCommit.ID})

	// Create D -> E
	dCommit := createTestModuleWithDeps(t, svc, "testowner", "d", []registry.File{
		{Path: "d.proto", Content: "syntax = \"proto3\";\npackage d;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/d"},
	}, []string{"main"}, []string{eCommit.ID})

	// Create C -> D
	cCommit := createTestModuleWithDeps(t, svc, "testowner", "c", []registry.File{
		{Path: "c.proto", Content: "syntax = \"proto3\";\npackage c;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/c"},
	}, []string{"main"}, []string{dCommit.ID})

	// Create B -> C
	bCommit := createTestModuleWithDeps(t, svc, "testowner", "b", []registry.File{
		{Path: "b.proto", Content: "syntax = \"proto3\";\npackage b;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/b"},
	}, []string{"main"}, []string{cCommit.ID})

	// Create A -> B
	aCommit := createTestModuleWithDeps(t, svc, "testowner", "a", []registry.File{
		{Path: "a.proto", Content: "syntax = \"proto3\";\npackage a;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/a"},
	}, []string{"main"}, []string{bCommit.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: aCommit.ID}}},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 6 commits (A, B, C, D, E, F)
	if len(resp.Msg.Graph.Commits) != 6 {
		t.Errorf("expected 6 commits, got %d", len(resp.Msg.Graph.Commits))
	}

	// Should have 5 edges (A->B, B->C, C->D, D->E, E->F)
	if len(resp.Msg.Graph.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(resp.Msg.Graph.Edges))
	}

	// Verify the chain
	edgeMap := make(map[string]string)
	for _, e := range resp.Msg.Graph.Edges {
		edgeMap[e.FromNode.CommitId] = e.ToNode.CommitId
	}

	if edgeMap[aCommit.ID] != bCommit.ID {
		t.Error("expected edge A -> B")
	}
	if edgeMap[bCommit.ID] != cCommit.ID {
		t.Error("expected edge B -> C")
	}
	if edgeMap[cCommit.ID] != dCommit.ID {
		t.Error("expected edge C -> D")
	}
	if edgeMap[dCommit.ID] != eCommit.ID {
		t.Error("expected edge D -> E")
	}
	if edgeMap[eCommit.ID] != fCommit.ID {
		t.Error("expected edge E -> F")
	}
}

// TestGetGraph_ThreeWayVersionConflict tests when three modules depend on three different versions
// of the same base module. The newest version should win.
func TestGetGraph_ThreeWayVersionConflict(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base@v1
	baseV1 := createTestModule(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V1 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v1"})

	// Create base@v2
	baseV2 := createTestModuleWithDeps(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V2 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v2"}, nil)

	// Create base@v3 (newest)
	baseV3 := createTestModuleWithDeps(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V3 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v3", "main"}, nil)

	// Create mid-a -> base@v1 (oldest)
	midA := createTestModuleWithDeps(t, svc, "testowner", "mida", []registry.File{
		{Path: "mida.proto", Content: "syntax = \"proto3\";\npackage mida;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/mida"},
	}, []string{"main"}, []string{baseV1.ID})

	// Create mid-b -> base@v3 (newest)
	midB := createTestModuleWithDeps(t, svc, "testowner", "midb", []registry.File{
		{Path: "midb.proto", Content: "syntax = \"proto3\";\npackage midb;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/midb"},
	}, []string{"main"}, []string{baseV3.ID})

	// Create mid-c -> base@v2 (middle)
	midC := createTestModuleWithDeps(t, svc, "testowner", "midc", []registry.File{
		{Path: "midc.proto", Content: "syntax = \"proto3\";\npackage midc;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/midc"},
	}, []string{"main"}, []string{baseV2.ID})

	// Create top -> [mid-a, mid-b, mid-c] (processing order: v1, v3, v2)
	top := createTestModuleWithDeps(t, svc, "testowner", "top", []registry.File{
		{Path: "top.proto", Content: "syntax = \"proto3\";\npackage top;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/top"},
	}, []string{"main"}, []string{midA.ID, midB.ID, midC.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: top.ID}}},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 5 commits: top, mid-a, mid-b, mid-c, base (deduplicated to v3)
	if len(resp.Msg.Graph.Commits) != 5 {
		t.Errorf("expected 5 commits, got %d", len(resp.Msg.Graph.Commits))
		for _, c := range resp.Msg.Graph.Commits {
			t.Logf("  commit: %s", c.Commit.Id)
		}
	}

	// Verify only base@v3 is in commits (v1 and v2 should be replaced)
	commitIDs := make(map[string]bool)
	for _, c := range resp.Msg.Graph.Commits {
		commitIDs[c.Commit.Id] = true
	}

	if commitIDs[baseV1.ID] {
		t.Error("base@v1 should not be in commits")
	}
	if commitIDs[baseV2.ID] {
		t.Error("base@v2 should not be in commits")
	}
	if !commitIDs[baseV3.ID] {
		t.Error("base@v3 (newest) should be in commits")
	}

	// All edges to base should point to v3
	for _, e := range resp.Msg.Graph.Edges {
		if e.ToNode.CommitId == baseV1.ID || e.ToNode.CommitId == baseV2.ID {
			t.Errorf("edge should point to base@v3, not %s", e.ToNode.CommitId)
		}
	}
}

// TestGetGraph_ComplexDiamond tests a more complex diamond with shared dependencies at multiple levels
// Structure:
//
//	    top
//	   / | \
//	  a  b  c
//	  |\ |\ |
//	  | \|/ |
//	  |  d  |
//	   \ | /
//	    base
func TestGetGraph_ComplexDiamond(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base
	base := createTestModule(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"main"})

	// Create d -> base
	d := createTestModuleWithDeps(t, svc, "testowner", "d", []registry.File{
		{Path: "d.proto", Content: "syntax = \"proto3\";\npackage d;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/d"},
	}, []string{"main"}, []string{base.ID})

	// Create a -> [d, base]
	a := createTestModuleWithDeps(t, svc, "testowner", "a", []registry.File{
		{Path: "a.proto", Content: "syntax = \"proto3\";\npackage a;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/a"},
	}, []string{"main"}, []string{d.ID, base.ID})

	// Create b -> [d]
	b := createTestModuleWithDeps(t, svc, "testowner", "b", []registry.File{
		{Path: "b.proto", Content: "syntax = \"proto3\";\npackage b;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/b"},
	}, []string{"main"}, []string{d.ID})

	// Create c -> [d, base]
	c := createTestModuleWithDeps(t, svc, "testowner", "c", []registry.File{
		{Path: "c.proto", Content: "syntax = \"proto3\";\npackage c;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/c"},
	}, []string{"main"}, []string{d.ID, base.ID})

	// Create top -> [a, b, c]
	top := createTestModuleWithDeps(t, svc, "testowner", "top", []registry.File{
		{Path: "top.proto", Content: "syntax = \"proto3\";\npackage top;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/top"},
	}, []string{"main"}, []string{a.ID, b.ID, c.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: top.ID}}},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 6 commits: top, a, b, c, d, base (each appearing once)
	if len(resp.Msg.Graph.Commits) != 6 {
		t.Errorf("expected 6 commits, got %d", len(resp.Msg.Graph.Commits))
		for _, c := range resp.Msg.Graph.Commits {
			t.Logf("  commit: %s", c.Commit.Id)
		}
	}

	// Verify each commit appears exactly once
	commitCounts := make(map[string]int)
	for _, c := range resp.Msg.Graph.Commits {
		commitCounts[c.Commit.Id]++
	}

	for id, count := range commitCounts {
		if count != 1 {
			t.Errorf("commit %s appears %d times, expected 1", id, count)
		}
	}

	// Should have edges: top->a, top->b, top->c, a->d, a->base, b->d, c->d, c->base, d->base
	// That's 9 edges
	if len(resp.Msg.Graph.Edges) != 9 {
		t.Errorf("expected 9 edges, got %d", len(resp.Msg.Graph.Edges))
		for _, e := range resp.Msg.Graph.Edges {
			t.Logf("  edge: %s -> %s", e.FromNode.CommitId, e.ToNode.CommitId)
		}
	}
}

// TestGetGraph_SameModuleRequestedTwice tests that requesting the same module twice
// doesn't result in duplicates
func TestGetGraph_SameModuleRequestedTwice(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a simple module
	commit := createTestModule(t, svc, "testowner", "module", []registry.File{
		{Path: "mod.proto", Content: "syntax = \"proto3\";\npackage mod;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/module"},
	}, []string{"main"})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: commit.ID}}},
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: commit.ID}}},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 2 commits (same module twice at root level - buf handles dedup)
	// The graph returns what was requested
	if len(resp.Msg.Graph.Commits) != 2 {
		t.Errorf("expected 2 commits (same module requested twice), got %d", len(resp.Msg.Graph.Commits))
	}
}

// TestGetGraph_PinnedByCommitID tests that dependencies pinned by commit ID are resolved correctly
func TestGetGraph_PinnedByCommitID(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base module with multiple versions
	baseV1 := createTestModule(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V1 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v1"})

	baseV2 := createTestModuleWithDeps(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V2 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v2", "main"}, nil)

	// Create consumer that pins to v1 by commit ID (even though v2 exists and is newer)
	consumer := createTestModuleWithDeps(t, svc, "testowner", "consumer", []registry.File{
		{Path: "consumer.proto", Content: "syntax = \"proto3\";\npackage consumer;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/consumer"},
	}, []string{"main"}, []string{baseV1.ID})

	ctx := contextWithUser(context.Background(), "testuser")

	// Request graph by consumer's commit ID - should return the pinned v1
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: consumer.ID}}},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 2 commits: consumer and base@v1
	if len(resp.Msg.Graph.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(resp.Msg.Graph.Commits))
	}

	// Verify base@v1 is in commits (not v2)
	commitIDs := make(map[string]bool)
	for _, c := range resp.Msg.Graph.Commits {
		commitIDs[c.Commit.Id] = true
	}

	if !commitIDs[baseV1.ID] {
		t.Error("expected base@v1 (pinned) to be in commits")
	}
	if commitIDs[baseV2.ID] {
		t.Error("base@v2 should not be in commits (consumer pinned to v1)")
	}
}

// TestGetGraph_PinnedByLabel tests that dependencies resolved by label work correctly
func TestGetGraph_PinnedByLabel(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base module with v1 label
	baseV1 := createTestModule(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V1 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v1.0.0"})

	// Create base module with v2 label (and update main)
	baseV2 := createTestModuleWithDeps(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\nmessage V2 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v2.0.0", "main"}, nil)

	ctx := contextWithUser(context.Background(), "testuser")

	// Request by label v1.0.0
	reqV1 := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Name_{
						Name: &v1beta1.ResourceRef_Name{
							Owner:  "testowner",
							Module: "base",
							Child:  &v1beta1.ResourceRef_Name_LabelName{LabelName: "v1.0.0"},
						},
					},
				},
			},
		},
	})

	respV1, err := svc.GetGraph(ctx, reqV1)
	if err != nil {
		t.Fatalf("GetGraph for v1.0.0 failed: %v", err)
	}

	if len(respV1.Msg.Graph.Commits) != 1 {
		t.Errorf("expected 1 commit for v1.0.0, got %d", len(respV1.Msg.Graph.Commits))
	}
	if respV1.Msg.Graph.Commits[0].Commit.Id != baseV1.ID {
		t.Errorf("expected commit ID %s for v1.0.0, got %s", baseV1.ID, respV1.Msg.Graph.Commits[0].Commit.Id)
	}

	// Request by label v2.0.0
	reqV2 := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Name_{
						Name: &v1beta1.ResourceRef_Name{
							Owner:  "testowner",
							Module: "base",
							Child:  &v1beta1.ResourceRef_Name_LabelName{LabelName: "v2.0.0"},
						},
					},
				},
			},
		},
	})

	respV2, err := svc.GetGraph(ctx, reqV2)
	if err != nil {
		t.Fatalf("GetGraph for v2.0.0 failed: %v", err)
	}

	if len(respV2.Msg.Graph.Commits) != 1 {
		t.Errorf("expected 1 commit for v2.0.0, got %d", len(respV2.Msg.Graph.Commits))
	}
	if respV2.Msg.Graph.Commits[0].Commit.Id != baseV2.ID {
		t.Errorf("expected commit ID %s for v2.0.0, got %s", baseV2.ID, respV2.Msg.Graph.Commits[0].Commit.Id)
	}
}

// TestGetGraph_RefResolution tests that Ref field can resolve both commit IDs and labels
func TestGetGraph_RefResolution(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create a module with a label
	commit := createTestModule(t, svc, "testowner", "testmod", []registry.File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/testmod"},
	}, []string{"main", "v1.0.0"})

	ctx := contextWithUser(context.Background(), "testuser")

	// Test resolving by commit ID via Ref field
	reqByCommitID := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Name_{
						Name: &v1beta1.ResourceRef_Name{
							Owner:  "testowner",
							Module: "testmod",
							Child:  &v1beta1.ResourceRef_Name_Ref{Ref: commit.ID},
						},
					},
				},
			},
		},
	})

	respByCommitID, err := svc.GetGraph(ctx, reqByCommitID)
	if err != nil {
		t.Fatalf("GetGraph by commit ID ref failed: %v", err)
	}

	if respByCommitID.Msg.Graph.Commits[0].Commit.Id != commit.ID {
		t.Errorf("ref by commit ID: expected %s, got %s", commit.ID, respByCommitID.Msg.Graph.Commits[0].Commit.Id)
	}

	// Test resolving by label via Ref field
	reqByLabel := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{
				ResourceRef: &v1beta1.ResourceRef{
					Value: &v1beta1.ResourceRef_Name_{
						Name: &v1beta1.ResourceRef_Name{
							Owner:  "testowner",
							Module: "testmod",
							Child:  &v1beta1.ResourceRef_Name_Ref{Ref: "v1.0.0"},
						},
					},
				},
			},
		},
	})

	respByLabel, err := svc.GetGraph(ctx, reqByLabel)
	if err != nil {
		t.Fatalf("GetGraph by label ref failed: %v", err)
	}

	if respByLabel.Msg.Graph.Commits[0].Commit.Id != commit.ID {
		t.Errorf("ref by label: expected %s, got %s", commit.ID, respByLabel.Msg.Graph.Commits[0].Commit.Id)
	}
}

// TestGetGraph_PinnedDepsPreservedInLockFile tests that when a module has pinned dependencies,
// the graph correctly represents the pinned versions even when newer versions exist
func TestGetGraph_PinnedDepsPreservedInLockFile(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create shared dependency with multiple versions
	sharedV1 := createTestModule(t, svc, "testowner", "shared", []registry.File{
		{Path: "shared.proto", Content: "syntax = \"proto3\";\npackage shared;\nmessage SharedV1 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/shared"},
	}, []string{"v1.0.0"})

	sharedV2 := createTestModuleWithDeps(t, svc, "testowner", "shared", []registry.File{
		{Path: "shared.proto", Content: "syntax = \"proto3\";\npackage shared;\nmessage SharedV2 {}"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/shared"},
	}, []string{"v2.0.0", "main"}, nil)

	// Create lib-a that explicitly pins to shared@v1.0.0
	libA := createTestModuleWithDeps(t, svc, "testowner", "liba", []registry.File{
		{Path: "liba.proto", Content: "syntax = \"proto3\";\npackage liba;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/liba"},
	}, []string{"main"}, []string{sharedV1.ID})

	// Create lib-b that uses shared@v2.0.0 (latest)
	libB := createTestModuleWithDeps(t, svc, "testowner", "libb", []registry.File{
		{Path: "libb.proto", Content: "syntax = \"proto3\";\npackage libb;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/libb"},
	}, []string{"main"}, []string{sharedV2.ID})

	ctx := contextWithUser(context.Background(), "testuser")

	// Get graph for lib-a - should have shared@v1
	reqLibA := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: libA.ID}}},
		},
	})

	respLibA, err := svc.GetGraph(ctx, reqLibA)
	if err != nil {
		t.Fatalf("GetGraph for lib-a failed: %v", err)
	}

	// Verify lib-a's graph has shared@v1
	libACommitIDs := make(map[string]bool)
	for _, c := range respLibA.Msg.Graph.Commits {
		libACommitIDs[c.Commit.Id] = true
	}
	if !libACommitIDs[sharedV1.ID] {
		t.Error("lib-a graph should contain shared@v1.0.0")
	}
	if libACommitIDs[sharedV2.ID] {
		t.Error("lib-a graph should not contain shared@v2.0.0")
	}

	// Get graph for lib-b - should have shared@v2
	reqLibB := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: libB.ID}}},
		},
	})

	respLibB, err := svc.GetGraph(ctx, reqLibB)
	if err != nil {
		t.Fatalf("GetGraph for lib-b failed: %v", err)
	}

	// Verify lib-b's graph has shared@v2
	libBCommitIDs := make(map[string]bool)
	for _, c := range respLibB.Msg.Graph.Commits {
		libBCommitIDs[c.Commit.Id] = true
	}
	if libBCommitIDs[sharedV1.ID] {
		t.Error("lib-b graph should not contain shared@v1.0.0")
	}
	if !libBCommitIDs[sharedV2.ID] {
		t.Error("lib-b graph should contain shared@v2.0.0")
	}
}

// TestGetGraph_TransitiveDependencyVersionConflict tests version resolution when
// the conflict is not at the direct dependency level but deeper in the tree
func TestGetGraph_TransitiveDependencyVersionConflict(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Create base@v1
	baseV1 := createTestModule(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\n// v1"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v1"})

	// Create base@v2 (newer)
	baseV2 := createTestModuleWithDeps(t, svc, "testowner", "base", []registry.File{
		{Path: "base.proto", Content: "syntax = \"proto3\";\npackage base;\n// v2"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/base"},
	}, []string{"v2", "main"}, nil)

	// Create lib-a -> base@v1
	libA := createTestModuleWithDeps(t, svc, "testowner", "liba", []registry.File{
		{Path: "liba.proto", Content: "syntax = \"proto3\";\npackage liba;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/liba"},
	}, []string{"main"}, []string{baseV1.ID})

	// Create lib-b -> base@v2
	libB := createTestModuleWithDeps(t, svc, "testowner", "libb", []registry.File{
		{Path: "libb.proto", Content: "syntax = \"proto3\";\npackage libb;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/libb"},
	}, []string{"main"}, []string{baseV2.ID})

	// Create mid -> [lib-a] (transitive dep on base@v1)
	mid := createTestModuleWithDeps(t, svc, "testowner", "mid", []registry.File{
		{Path: "mid.proto", Content: "syntax = \"proto3\";\npackage mid;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/mid"},
	}, []string{"main"}, []string{libA.ID})

	// Create top -> [mid, lib-b]
	// mid transitively depends on base@v1
	// lib-b directly depends on base@v2
	// base@v2 should win
	top := createTestModuleWithDeps(t, svc, "testowner", "top", []registry.File{
		{Path: "top.proto", Content: "syntax = \"proto3\";\npackage top;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/top"},
	}, []string{"main"}, []string{mid.ID, libB.ID})

	ctx := contextWithUser(context.Background(), "testuser")
	req := connect.NewRequest(&v1beta1.GetGraphRequest{
		ResourceRefs: []*v1beta1.GetGraphRequest_ResourceRef{
			{ResourceRef: &v1beta1.ResourceRef{Value: &v1beta1.ResourceRef_Id{Id: top.ID}}},
		},
	})

	resp, err := svc.GetGraph(ctx, req)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// Should have 5 commits: top, mid, lib-a, lib-b, base (deduplicated to v2)
	if len(resp.Msg.Graph.Commits) != 5 {
		t.Errorf("expected 5 commits, got %d", len(resp.Msg.Graph.Commits))
		for _, c := range resp.Msg.Graph.Commits {
			t.Logf("  commit: %s", c.Commit.Id)
		}
	}

	// Verify base@v2 is selected, not base@v1
	commitIDs := make(map[string]bool)
	for _, c := range resp.Msg.Graph.Commits {
		commitIDs[c.Commit.Id] = true
	}

	if commitIDs[baseV1.ID] {
		t.Error("base@v1 should not be in commits (should be replaced by v2)")
	}
	if !commitIDs[baseV2.ID] {
		t.Error("base@v2 (newer) should be in commits")
	}
}
