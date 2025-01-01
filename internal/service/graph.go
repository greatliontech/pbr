package service

import (
	"context"
	"fmt"
	"strings"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (reg *Service) GetGraph(ctx context.Context, req *connect.Request[v1beta1.GetGraphRequest]) (*connect.Response[v1beta1.GetGraphResponse], error) {
	resp := &connect.Response[v1beta1.GetGraphResponse]{}
	resp.Msg = &v1beta1.GetGraphResponse{
		Graph: &v1beta1.Graph{},
	}

	commitMap := map[string]*v1beta1.Commit{}
	modules := []*registry.Module{}

	for _, ref := range req.Msg.ResourceRefs {
		switch ref := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			mod, err := reg.reg.ModuleByCommitID(ctx, ref.Id)
			if err != nil {
				return nil, err
			}
			modules = append(modules, mod)
			cmt, err := mod.CommitById(ref.Id)
			if err != nil {
				return nil, err
			}
			commit, err := getCommitObject(cmt.OwnerId, cmt.ModuleId, cmt.CommitId, cmt.Digest)
			if err != nil {
				return nil, err
			}
			key := mod.Name + "/" + mod.Owner
			commitMap[key] = commit
			resp.Msg.Graph.Commits = append(resp.Msg.Graph.Commits, &v1beta1.Graph_Commit{
				Commit:   commit,
				Registry: reg.conf.Host,
			})
		case *v1beta1.ResourceRef_Name_:
			return nil, fmt.Errorf("ResourceRef_Name_ not supported")
		}
	}

	for i, mod := range modules {
		if err := reg.getGraph(ctx, mod, resp.Msg.Graph.Commits[i].Commit, commitMap, resp.Msg.Graph); err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func (reg *Service) getGraph(ctx context.Context, mod *registry.Module, commit *v1beta1.Commit, commits map[string]*v1beta1.Commit, graph *v1beta1.Graph) error {
	ctx, span := tracer.Start(ctx, "getGraph", trace.WithAttributes(
		attribute.String("owner", mod.Owner),
		attribute.String("module", mod.Name),
		attribute.String("commit", commit.Id),
	))
	defer span.End()

	bl, err := mod.BufLockCommitId(ctx, commit.Id)
	if err != nil {
		if err == registry.ErrBufLockNotFound {
			// no dependencies
			return nil
		}
		return err
	}
	for _, dep := range bl.Deps {
		var depCommit *v1beta1.Commit
		key := dep.Owner + "/" + dep.Repository
		if dc, ok := commits[key]; ok {
			depCommit = dc
		} else {
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
				Registry: reg.conf.Host,
			},
			ToNode: &v1beta1.Graph_Node{
				CommitId: depCommit.Id,
				Registry: dep.Remote,
			},
		})
		if dep.Remote == reg.conf.Host {
			mod, err := reg.reg.Module(ctx, dep.Owner, dep.Repository)
			if err != nil {
				return err
			}
			err = reg.getGraph(ctx, mod, depCommit, commits, graph)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
