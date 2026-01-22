package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GraphServiceV1 implements the v1 GraphService interface by wrapping Service.
type GraphServiceV1 struct {
	svc *Service
}

// NewGraphServiceV1 creates a new v1 GraphService wrapper.
func NewGraphServiceV1(svc *Service) *GraphServiceV1 {
	return &GraphServiceV1{svc: svc}
}

// GetGraph retrieves a dependency graph for the given resource references.
// This v1 endpoint returns commits with B5 digests (instead of B4 in v1beta1).
func (g *GraphServiceV1) GetGraph(ctx context.Context, req *connect.Request[v1.GetGraphRequest]) (*connect.Response[v1.GetGraphResponse], error) {
	if g.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	user := userFromContext(ctx)
	slog.DebugContext(ctx, "GetGraphV1", "user", user)

	resp := &connect.Response[v1.GetGraphResponse]{}
	resp.Msg = &v1.GetGraphResponse{
		Graph: &v1.Graph{},
	}

	commitMap := map[string]*v1.Commit{}

	type moduleEntry struct {
		info   moduleInfo
		commit *v1.Commit
	}
	modules := []moduleEntry{}

	for _, ref := range req.Msg.ResourceRefs {
		switch r := ref.Value.(type) {
		case *v1.ResourceRef_Id:
			info, commit, err := g.getModuleAndCommitByIDV1(ctx, r.Id)
			if err != nil {
				slog.ErrorContext(ctx, "getModuleAndCommitByIDV1", "err", err)
				return nil, err
			}
			modules = append(modules, moduleEntry{info: info, commit: commit})
			key := info.Owner + "/" + info.Name
			commitMap[key] = commit
			slog.DebugContext(ctx, "top level dep v1", "id", commit.Id)
			resp.Msg.Graph.Commits = append(resp.Msg.Graph.Commits, commit)
		case *v1.ResourceRef_Name_:
			info, commit, err := g.getModuleAndCommitByNameV1(ctx, r.Name)
			if err != nil {
				slog.ErrorContext(ctx, "getModuleAndCommitByNameV1", "err", err)
				return nil, err
			}
			modules = append(modules, moduleEntry{info: info, commit: commit})
			key := info.Owner + "/" + info.Name
			commitMap[key] = commit
			slog.DebugContext(ctx, "top level dep v1", "id", commit.Id)
			resp.Msg.Graph.Commits = append(resp.Msg.Graph.Commits, commit)
		}
	}

	for i, entry := range modules {
		if err := g.getGraphForModuleV1(ctx, entry.info, resp.Msg.Graph.Commits[i], commitMap, resp.Msg.Graph); err != nil {
			return nil, err
		}
	}

	// debug print of the graph
	for _, cmt := range commitMap {
		slog.DebugContext(ctx, "commit v1", "id", cmt.Id)
	}
	for _, edge := range resp.Msg.Graph.Edges {
		slog.DebugContext(ctx, "edge v1", "from", edge.FromNode.CommitId, "to", edge.ToNode.CommitId)
	}

	return resp, nil
}

func (g *GraphServiceV1) getModuleAndCommitByIDV1(ctx context.Context, commitID string) (moduleInfo, *v1.Commit, error) {
	mod, err := g.svc.casReg.ModuleByCommitID(ctx, commitID)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	cmt, err := mod.CommitByID(ctx, commitID)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	commit := getCommitObjectV1(cmt)
	return moduleInfo{Owner: mod.Owner(), Name: mod.Name()}, commit, nil
}

func (g *GraphServiceV1) getModuleAndCommitByNameV1(ctx context.Context, name *v1.ResourceRef_Name) (moduleInfo, *v1.Commit, error) {
	if name == nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	owner := name.Owner
	modName := name.Module

	mod, err := g.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
	}

	var cmt *registry.Commit
	switch child := name.Child.(type) {
	case *v1.ResourceRef_Name_LabelName:
		cmt, err = mod.Commit(ctx, child.LabelName)
		if err != nil {
			return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("label not found: %s", child.LabelName))
		}
	case *v1.ResourceRef_Name_Ref:
		// Ref could be a commit ID or a label name - try commit ID first
		cmt, err = mod.CommitByID(ctx, child.Ref)
		if err != nil {
			// Try as a label
			cmt, err = mod.Commit(ctx, child.Ref)
			if err != nil {
				return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref not found: %s", child.Ref))
			}
		}
	default:
		// No child specified - use default label (main)
		cmt, err = mod.Commit(ctx, "main")
		if err != nil {
			return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, errors.New("default label 'main' not found"))
		}
	}

	commit := getCommitObjectV1(cmt)
	return moduleInfo{Owner: owner, Name: modName}, commit, nil
}

func (g *GraphServiceV1) getGraphForModuleV1(ctx context.Context, info moduleInfo, commit *v1.Commit, commits map[string]*v1.Commit, graph *v1.Graph) error {
	ctx, span := tracer.Start(ctx, "service.getGraphForModuleV1", trace.WithAttributes(
		attribute.String("owner", info.Owner),
		attribute.String("module", info.Name),
		attribute.String("commit", commit.Id),
	))
	defer span.End()

	slog.DebugContext(ctx, "getGraphForModuleV1", "owner", info.Owner, "module", info.Name, "commit", commit.Id)

	// Get dependencies from stored commit metadata
	deps, err := g.svc.getStoredDeps(ctx, info.Owner, info.Name, commit.Id)
	if err != nil {
		return err
	}

	if len(deps) == 0 {
		slog.DebugContext(ctx, "no dependencies v1")
		return nil
	}

	for _, dep := range deps {
		slog.DebugContext(ctx, "dep v1", "owner", dep.Owner, "repo", dep.Repository, "commit", dep.Commit, "digest", dep.Digest)

		// Use module name as key for deduplication
		moduleKey := dep.Owner + "/" + dep.Repository

		// Get the commit info for B5 digest
		depCommit := g.createV1CommitFromDep(dep)

		// Check if we've already seen this module
		existingCommit, alreadySeen := commits[moduleKey]
		if alreadySeen {
			if existingCommit.Id != depCommit.Id {
				// Version conflict - compare commit IDs directly
				if depCommit.Id > existingCommit.Id {
					slog.DebugContext(ctx, "module version conflict v1, new is newer", "key", moduleKey, "old", existingCommit.Id, "new", depCommit.Id)
					commits[moduleKey] = depCommit
					// Update the commit in the graph
					for i, gc := range graph.Commits {
						if gc.Id == existingCommit.Id {
							graph.Commits[i] = depCommit
							break
						}
					}
					// Update any existing edges that pointed to the old commit
					for i, edge := range graph.Edges {
						if edge.ToNode.CommitId == existingCommit.Id {
							graph.Edges[i].ToNode.CommitId = depCommit.Id
						}
					}
				} else {
					slog.DebugContext(ctx, "module version conflict v1, keeping existing (newer)", "key", moduleKey, "existing", existingCommit.Id, "new", depCommit.Id)
				}
			} else {
				slog.DebugContext(ctx, "module already in commits v1 with same version", "key", moduleKey)
			}
		} else {
			slog.DebugContext(ctx, "adding module to commits v1", "key", moduleKey, "commit", dep.Commit)
			commits[moduleKey] = depCommit
			graph.Commits = append(graph.Commits, depCommit)
		}

		// Add edge to the current resolved commit
		graph.Edges = append(graph.Edges, &v1.Graph_Edge{
			FromNode: &v1.Graph_Node{
				CommitId: commit.Id,
			},
			ToNode: &v1.Graph_Node{
				CommitId: commits[moduleKey].Id,
			},
		})

		// Recurse into dependencies if this is a local module and we haven't processed this version yet
		if dep.Remote == g.svc.conf.Host && !alreadySeen {
			err = g.getGraphForModuleV1(ctx, moduleInfo{Owner: dep.Owner, Name: dep.Repository}, depCommit, commits, graph)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// createV1CommitFromDep creates a v1.Commit from a bufLockDep.
// For v1 API, we need to look up the actual commit to get the B5 module digest.
func (g *GraphServiceV1) createV1CommitFromDep(dep bufLockDep) *v1.Commit {
	ownerId := util.OwnerID(dep.Owner)
	modId := util.ModuleID(ownerId, dep.Repository)

	// Try to get the actual commit to get the B5 module digest
	if mod, err := g.svc.casReg.Module(context.Background(), dep.Owner, dep.Repository); err == nil {
		if cmt, err := mod.CommitByID(context.Background(), dep.Commit); err == nil {
			return getCommitObjectV1(cmt)
		}
	}

	// Fallback: create commit with files digest as B5
	// This is for external deps or when lookup fails
	digestHex := strings.TrimPrefix(dep.Digest, "shake256:")
	digestHex = strings.TrimPrefix(digestHex, "b5:")
	digestBytes, _ := hex.DecodeString(digestHex)

	return &v1.Commit{
		Id:       dep.Commit,
		OwnerId:  ownerId,
		ModuleId: modId,
		Digest: &v1.Digest{
			Type:  v1.DigestType_DIGEST_TYPE_B5,
			Value: digestBytes,
		},
	}
}

// getCommitObjectV1 creates a v1.Commit object with B5 digest from a registry.Commit.
func getCommitObjectV1(commit *registry.Commit) *v1.Commit {
	return &v1.Commit{
		Id:         commit.ID,
		OwnerId:    commit.OwnerID,
		ModuleId:   commit.ModuleID,
		CreateTime: timestamppb.New(commit.CreateTime),
		Digest: &v1.Digest{
			Type:  v1.DigestType_DIGEST_TYPE_B5,
			Value: commit.ModuleDigest.Value,
		},
	}
}
