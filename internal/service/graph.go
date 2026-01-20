package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry/cas"
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// moduleInfo holds common module info
type moduleInfo struct {
	Owner string
	Name  string
}

func (svc *Service) GetGraph(ctx context.Context, req *connect.Request[v1beta1.GetGraphRequest]) (*connect.Response[v1beta1.GetGraphResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	user := userFromContext(ctx)
	slog.DebugContext(ctx, "GetGraph", "user", user)

	resp := &connect.Response[v1beta1.GetGraphResponse]{}
	resp.Msg = &v1beta1.GetGraphResponse{
		Graph: &v1beta1.Graph{},
	}

	commitMap := map[string]*v1beta1.Commit{}

	type moduleEntry struct {
		info   moduleInfo
		commit *v1beta1.Commit
	}
	modules := []moduleEntry{}

	for _, ref := range req.Msg.ResourceRefs {
		switch r := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			info, commit, err := svc.getModuleAndCommitByID(ctx, r.Id)
			if err != nil {
				slog.ErrorContext(ctx, "getModuleAndCommitByID", "err", err)
				return nil, err
			}
			modules = append(modules, moduleEntry{info: info, commit: commit})
			key := info.Owner + "/" + info.Name
			commitMap[key] = commit
			slog.DebugContext(ctx, "top level dep", "id", commit.Id)
			resp.Msg.Graph.Commits = append(resp.Msg.Graph.Commits, &v1beta1.Graph_Commit{
				Commit:   commit,
				Registry: svc.conf.Host,
			})
		case *v1beta1.ResourceRef_Name_:
			info, commit, err := svc.getModuleAndCommitByName(ctx, r.Name)
			if err != nil {
				slog.ErrorContext(ctx, "getModuleAndCommitByName", "err", err)
				return nil, err
			}
			modules = append(modules, moduleEntry{info: info, commit: commit})
			key := info.Owner + "/" + info.Name
			commitMap[key] = commit
			slog.DebugContext(ctx, "top level dep", "id", commit.Id)
			resp.Msg.Graph.Commits = append(resp.Msg.Graph.Commits, &v1beta1.Graph_Commit{
				Commit:   commit,
				Registry: svc.conf.Host,
			})
		}
	}

	for i, entry := range modules {
		if err := svc.getGraphForModule(ctx, entry.info, resp.Msg.Graph.Commits[i].Commit, commitMap, resp.Msg.Graph); err != nil {
			return nil, err
		}
	}

	// debug print of the graph
	for _, cmt := range commitMap {
		slog.DebugContext(ctx, "commit", "id", cmt.Id)
	}
	for _, edge := range resp.Msg.Graph.Edges {
		slog.DebugContext(ctx, "edge", "from", edge.FromNode.CommitId, "to", edge.ToNode.CommitId)
	}

	return resp, nil
}

func (svc *Service) getModuleAndCommitByID(ctx context.Context, commitID string) (moduleInfo, *v1beta1.Commit, error) {
	mod, err := svc.casReg.ModuleByCommitID(ctx, commitID)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	cmt, err := mod.CommitByID(ctx, commitID)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	commit, err := getCommitObject(cmt.OwnerID, cmt.ModuleID, cmt.ID, cmt.ManifestDigest.Hex())
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	return moduleInfo{Owner: mod.Owner(), Name: mod.Name()}, commit, nil
}

func (svc *Service) getModuleAndCommitByName(ctx context.Context, name *v1beta1.ResourceRef_Name) (moduleInfo, *v1beta1.Commit, error) {
	if name == nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	owner := name.Owner
	modName := name.Module

	mod, err := svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
	}

	// Determine the ref (label name or commit ref)
	var cmt *cas.Commit
	switch child := name.Child.(type) {
	case *v1beta1.ResourceRef_Name_LabelName:
		cmt, err = mod.Commit(ctx, child.LabelName)
		if err != nil {
			return moduleInfo{}, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("label not found: %s", child.LabelName))
		}
	case *v1beta1.ResourceRef_Name_Ref:
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

	commit, err := getCommitObject(cmt.OwnerID, cmt.ModuleID, cmt.ID, cmt.ManifestDigest.Hex())
	if err != nil {
		return moduleInfo{}, nil, connect.NewError(connect.CodeInternal, err)
	}

	return moduleInfo{Owner: owner, Name: modName}, commit, nil
}

func (svc *Service) getGraphForModule(ctx context.Context, info moduleInfo, commit *v1beta1.Commit, commits map[string]*v1beta1.Commit, graph *v1beta1.Graph) error {
	ctx, span := tracer.Start(ctx, "service.getGraphForModule", trace.WithAttributes(
		attribute.String("owner", info.Owner),
		attribute.String("module", info.Name),
		attribute.String("commit", commit.Id),
	))
	defer span.End()

	slog.DebugContext(ctx, "getGraphForModule", "owner", info.Owner, "module", info.Name, "commit", commit.Id)

	// Get dependencies from stored commit metadata
	deps, err := svc.getStoredDeps(ctx, info.Owner, info.Name, commit.Id)
	if err != nil {
		return err
	}

	if len(deps) == 0 {
		slog.DebugContext(ctx, "no dependencies")
		return nil
	}

	for _, dep := range deps {
		slog.DebugContext(ctx, "dep", "owner", dep.Owner, "repo", dep.Repository, "commit", dep.Commit, "digest", dep.Digest)
		var depCommit *v1beta1.Commit
		key := dep.Owner + "/" + dep.Repository
		if dc, ok := commits[key]; ok {
			slog.DebugContext(ctx, "dep already in commits", "key", key)
			depCommit = dc
		} else {
			slog.DebugContext(ctx, "dep not in commits", "key", key)
			ownerId := util.OwnerID(dep.Owner)
			modId := util.ModuleID(ownerId, dep.Repository)
			depCommit, err = getCommitObject(ownerId, modId, dep.Commit, strings.TrimPrefix(dep.Digest, "shake256:"))
			if err != nil {
				return err
			}
			commits[key] = depCommit
			graph.Commits = append(graph.Commits, &v1beta1.Graph_Commit{
				Commit:   depCommit,
				Registry: dep.Remote,
			})
		}
		graph.Edges = append(graph.Edges, &v1beta1.Graph_Edge{
			FromNode: &v1beta1.Graph_Node{
				CommitId: commit.Id,
				Registry: svc.conf.Host,
			},
			ToNode: &v1beta1.Graph_Node{
				CommitId: depCommit.Id,
				Registry: dep.Remote,
			},
		})
		if dep.Remote == svc.conf.Host {
			err = svc.getGraphForModule(ctx, moduleInfo{Owner: dep.Owner, Name: dep.Repository}, depCommit, commits, graph)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// bufLockDep represents a dependency (from stored commit metadata or buf.lock)
type bufLockDep struct {
	Remote     string
	Owner      string
	Repository string
	Commit     string
	Digest     string
}

// getStoredDeps retrieves dependencies from the commit's stored DepCommitIDs.
func (svc *Service) getStoredDeps(ctx context.Context, owner, name, commitID string) ([]bufLockDep, error) {
	mod, err := svc.casReg.Module(ctx, owner, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, name))
	}

	commit, err := mod.CommitByID(ctx, commitID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	if len(commit.DepCommitIDs) == 0 {
		return nil, nil
	}

	deps := make([]bufLockDep, 0, len(commit.DepCommitIDs))
	for _, depCommitID := range commit.DepCommitIDs {
		// Look up the dependency commit to get module info
		depMod, err := svc.casReg.ModuleByCommitID(ctx, depCommitID)
		if err != nil {
			slog.DebugContext(ctx, "dependency commit not found locally", "depCommitID", depCommitID)
			continue
		}

		depCommit, err := depMod.CommitByID(ctx, depCommitID)
		if err != nil {
			slog.DebugContext(ctx, "failed to get dependency commit", "depCommitID", depCommitID, "error", err)
			continue
		}

		deps = append(deps, bufLockDep{
			Remote:     svc.conf.Host,
			Owner:      depMod.Owner(),
			Repository: depMod.Name(),
			Commit:     depCommitID,
			Digest:     "shake256:" + depCommit.ManifestDigest.Hex(),
		})
	}

	return deps, nil
}

var errBufLockNotFound = fmt.Errorf("buf.lock not found")

func (svc *Service) getBufLockDeps(ctx context.Context, owner, name, commitID string) ([]bufLockDep, error) {
	mod, err := svc.casReg.Module(ctx, owner, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, name))
	}

	bufLock, err := mod.BufLockCommitID(ctx, commitID)
	if err != nil {
		if err.Error() == "buf.lock not found" {
			return nil, errBufLockNotFound
		}
		return nil, err
	}

	return convertBufLockDeps(bufLock), nil
}

func convertBufLockDeps(bl *cas.BufLock) []bufLockDep {
	deps := make([]bufLockDep, len(bl.Deps))
	for i, d := range bl.Deps {
		deps[i] = bufLockDep{
			Remote:     d.Remote,
			Owner:      d.Owner,
			Repository: d.Repository,
			Commit:     d.Commit,
			Digest:     d.Digest,
		}
	}
	return deps
}
