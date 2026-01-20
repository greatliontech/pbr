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
	"github.com/greatliontech/pbr/internal/registry/cas"
	"github.com/greatliontech/pbr/internal/util"
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

// GetGraph gets a dependency graph that includes the given Commits.
func (g *GraphServiceV1) GetGraph(
	ctx context.Context,
	req *connect.Request[v1.GetGraphRequest],
) (*connect.Response[v1.GetGraphResponse], error) {
	if g.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "GetGraphV1", "resourceRefs", len(req.Msg.ResourceRefs))

	resp := &v1.GetGraphResponse{
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
			info, commit, err := g.getModuleAndCommitByID(ctx, r.Id)
			if err != nil {
				slog.ErrorContext(ctx, "getModuleAndCommitByID", "err", err)
				return nil, err
			}
			modules = append(modules, moduleEntry{info: info, commit: commit})
			key := info.Owner + "/" + info.Name
			commitMap[key] = commit
			slog.DebugContext(ctx, "top level dep", "id", commit.Id)
			resp.Graph.Commits = append(resp.Graph.Commits, commit)

		case *v1.ResourceRef_Name_:
			info, commit, err := g.getModuleAndCommitByName(ctx, r.Name)
			if err != nil {
				slog.ErrorContext(ctx, "getModuleAndCommitByName", "err", err)
				return nil, err
			}
			modules = append(modules, moduleEntry{info: info, commit: commit})
			key := info.Owner + "/" + info.Name
			commitMap[key] = commit
			slog.DebugContext(ctx, "top level dep", "id", commit.Id)
			resp.Graph.Commits = append(resp.Graph.Commits, commit)
		}
	}

	for _, entry := range modules {
		if err := g.getGraphForModule(ctx, entry.info, entry.commit, commitMap, resp.Graph); err != nil {
			return nil, err
		}
	}

	return connect.NewResponse(resp), nil
}

func (g *GraphServiceV1) getModuleAndCommitByID(ctx context.Context, commitID string) (moduleInfo, *v1.Commit, error) {
	mod, err := g.svc.casReg.ModuleByCommitID(ctx, commitID)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	cmt, err := mod.CommitByID(ctx, commitID)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	commit, err := g.commitToProto(cmt)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	return moduleInfo{Owner: mod.Owner(), Name: mod.Name()}, commit, nil
}

func (g *GraphServiceV1) getModuleAndCommitByName(ctx context.Context, name *v1.ResourceRef_Name) (moduleInfo, *v1.Commit, error) {
	if name == nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	owner := name.Owner
	modName := name.Module

	mod, err := g.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
	}

	// Determine the ref (label name or commit ref)
	var ref string
	switch child := name.Child.(type) {
	case *v1.ResourceRef_Name_LabelName:
		ref = child.LabelName
	case *v1.ResourceRef_Name_Ref:
		ref = child.Ref
	default:
		// No child specified - use default label (main)
		ref = "main"
	}

	cmt, err := mod.Commit(ctx, ref)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref not found: %s", ref))
	}

	commit, err := g.commitToProto(cmt)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	return moduleInfo{Owner: owner, Name: modName}, commit, nil
}

func (g *GraphServiceV1) getGraphForModule(ctx context.Context, info moduleInfo, commit *v1.Commit, commits map[string]*v1.Commit, graph *v1.Graph) error {
	slog.DebugContext(ctx, "getGraphForModule v1", "owner", info.Owner, "module", info.Name, "commit", commit.Id)

	// Get buf.lock
	deps, err := g.svc.getBufLockDeps(ctx, info.Owner, info.Name, commit.Id)
	if err != nil {
		if err == errBufLockNotFound {
			slog.DebugContext(ctx, "no dependencies")
			return nil
		}
		return err
	}

	for _, dep := range deps {
		slog.DebugContext(ctx, "dep", "owner", dep.Owner, "repo", dep.Repository, "commit", dep.Commit, "digest", dep.Digest)
		var depCommit *v1.Commit
		key := dep.Owner + "/" + dep.Repository
		if dc, ok := commits[key]; ok {
			slog.DebugContext(ctx, "dep already in commits", "key", key)
			depCommit = dc
		} else {
			slog.DebugContext(ctx, "dep not in commits", "key", key)
			ownerId := util.OwnerID(dep.Owner)
			modId := util.ModuleID(ownerId, dep.Repository)
			digest, err := hex.DecodeString(strings.TrimPrefix(dep.Digest, "shake256:"))
			if err != nil {
				return err
			}
			depCommit = &v1.Commit{
				Id:       dep.Commit,
				OwnerId:  ownerId,
				ModuleId: modId,
				Digest: &v1.Digest{
					Type:  v1.DigestType_DIGEST_TYPE_B5,
					Value: digest,
				},
			}
			commits[key] = depCommit
			graph.Commits = append(graph.Commits, depCommit)
		}
		graph.Edges = append(graph.Edges, &v1.Graph_Edge{
			FromNode: &v1.Graph_Node{
				CommitId: commit.Id,
			},
			ToNode: &v1.Graph_Node{
				CommitId: depCommit.Id,
			},
		})
		if dep.Remote == g.svc.conf.Host {
			err = g.getGraphForModule(ctx, moduleInfo{Owner: dep.Owner, Name: dep.Repository}, depCommit, commits, graph)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *GraphServiceV1) commitToProto(commit *cas.Commit) (*v1.Commit, error) {
	digest, err := hex.DecodeString(commit.ManifestDigest.Hex())
	if err != nil {
		return nil, fmt.Errorf("failed to decode digest: %w", err)
	}

	return &v1.Commit{
		Id:         commit.ID,
		OwnerId:    commit.OwnerID,
		ModuleId:   commit.ModuleID,
		CreateTime: timestamppb.New(commit.CreateTime),
		Digest: &v1.Digest{
			Type:  v1.DigestType_DIGEST_TYPE_B5,
			Value: digest,
		},
	}, nil
}
