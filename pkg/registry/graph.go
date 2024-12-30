package registry

import (
	"context"
	"fmt"
	"strings"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry"
)

func (reg *Registry) GetGraph(ctx context.Context, req *connect.Request[v1beta1.GetGraphRequest]) (*connect.Response[v1beta1.GetGraphResponse], error) {
	resp := &connect.Response[v1beta1.GetGraphResponse]{}
	resp.Msg = &v1beta1.GetGraphResponse{
		Graph: &v1beta1.Graph{},
	}

	commitMap := map[string]*v1beta1.Commit{}

	for _, ref := range req.Msg.ResourceRefs {
		switch ref := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			commit := reg.commits[ref.Id]
			// if commit not cached, loop all repos and find the sha!!!
			mod := reg.commitToModule[ref.Id]
			key := mod.Owner + "/" + mod.Module
			commitMap[key] = commit
			resp.Msg.Graph.Commits = append(resp.Msg.Graph.Commits, &v1beta1.Graph_Commit{
				Commit:   commit,
				Registry: reg.hostName,
			})
		case *v1beta1.ResourceRef_Name_:
			return nil, fmt.Errorf("ResourceRef_Name_ not supported")
		}
	}

	for _, commit := range resp.Msg.Graph.Commits {
		mod := reg.commitToModule[commit.Commit.Id]
		if err := reg.getGraph(mod, commit.Commit, commitMap, resp.Msg.Graph); err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func (reg *Registry) getGraph(mod *internalModule, commit *v1beta1.Commit, commits map[string]*v1beta1.Commit, graph *v1beta1.Graph) error {
	modl, err := reg.getModule(mod.Owner, mod.Module)
	if err != nil {
		return err
	}
	bl, err := modl.BufLockCommit(commit.Id)
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
			fmt.Printf("Dep in deps %s not in map, adding", key)
			ownerId := fakeUUID(dep.Owner)
			modId := fakeUUID(ownerId + "/" + dep.Repository)
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
				Registry: reg.hostName,
			},
			ToNode: &v1beta1.Graph_Node{
				CommitId: depCommit.Id,
				Registry: dep.Remote,
			},
		})
		reg.commitToModule[depCommit.Id] = &internalModule{Module: dep.Repository, Owner: dep.Owner}
		reg.commits[depCommit.Id] = depCommit
		if dep.Remote == reg.hostName {
			err := reg.getGraph(&internalModule{Module: dep.Repository, Owner: dep.Owner}, depCommit, commits, graph)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
