package service

import (
	"context"
	"os"
	"testing"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry/cas"
	"github.com/greatliontech/pbr/internal/storage/filesystem"
)

func setupTestService(t *testing.T) (*Service, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "graph-service-test-*")
	if err != nil {
		t.Fatal(err)
	}

	blobStore := filesystem.NewBlobStore(tmpDir + "/blobs")
	manifestStore := filesystem.NewManifestStore(tmpDir + "/manifests")
	metadataStore := filesystem.NewMetadataStore(tmpDir + "/metadata")

	casReg := cas.New(blobStore, manifestStore, metadataStore, "test.registry.com")

	svc := &Service{
		conf: &config.Config{
			Host: "test.registry.com",
		},
		casReg: casReg,
		tokens: map[string]string{"testtoken": "testuser"},
		users:  map[string]string{"testuser": "testtoken"},
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return svc, cleanup
}

func createTestModule(t *testing.T, svc *Service, owner, name string, files []cas.File, labels []string) *cas.Commit {
	t.Helper()
	return createTestModuleWithDeps(t, svc, owner, name, files, labels, nil)
}

func createTestModuleWithDeps(t *testing.T, svc *Service, owner, name string, files []cas.File, labels []string, depCommitIDs []string) *cas.Commit {
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
	files := []cas.File{
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
	depFiles := []cas.File{
		{Path: "dep.proto", Content: "syntax = \"proto3\";\npackage dep;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/depmodule"},
	}
	depCommit := createTestModule(t, svc, "testowner", "depmodule", depFiles, []string{"main"})

	// Create main module with dependency (pass depCommitIDs)
	mainFiles := []cas.File{
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
	cFiles := []cas.File{
		{Path: "c.proto", Content: "syntax = \"proto3\";\npackage c;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/modulec"},
	}
	cCommit := createTestModule(t, svc, "testowner", "modulec", cFiles, []string{"main"})

	// Create B (depends on C)
	bFiles := []cas.File{
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
	aFiles := []cas.File{
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

func TestGetGraph_DiamondDependencies(t *testing.T) {
	svc, cleanup := setupTestService(t)
	defer cleanup()

	// Diamond pattern: A -> B, A -> C, B -> D, C -> D
	// D (base, no deps)
	dFiles := []cas.File{
		{Path: "d.proto", Content: "syntax = \"proto3\";\npackage d;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/moduled"},
	}
	dCommit := createTestModule(t, svc, "testowner", "moduled", dFiles, []string{"main"})

	// B (depends on D)
	bFiles := []cas.File{
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
	cFiles := []cas.File{
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
	aFiles := []cas.File{
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
	mainFiles := []cas.File{
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
	files1 := []cas.File{
		{Path: "mod1.proto", Content: "syntax = \"proto3\";\npackage mod1;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/module1"},
	}
	commit1 := createTestModule(t, svc, "testowner", "module1", files1, []string{"main"})

	files2 := []cas.File{
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
	files := []cas.File{
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
