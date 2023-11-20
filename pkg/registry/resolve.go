package registry

import (
	"context"
	"fmt"

	modulev1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/module/v1alpha1"
	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (reg *Registry) GetModulePins(ctx context.Context, req *connect.Request[registryv1alpha1.GetModulePinsRequest]) (*connect.Response[registryv1alpha1.GetModulePinsResponse], error) {
	resp := &connect.Response[registryv1alpha1.GetModulePinsResponse]{}
	resp.Msg = &registryv1alpha1.GetModulePinsResponse{}

	reqPerRemote := requeststPerRemote(req)

	for remote, req := range reqPerRemote {
		if remote == reg.conf.Host {
			out, err := reg.handleLocalGetModulePins(ctx, req)
			if err != nil {
				fmt.Println("local get module pins error", err)
				continue
			}
			fmt.Println("local get module pins success", out)
			resp.Msg.ModulePins = append(resp.Msg.ModulePins, out.Msg.ModulePins...)
			continue
		}
		rmtSvc, ok := reg.remotes[remote]
		if !ok {
			fmt.Println("remote not found", remote)
			continue
		}
		fmt.Println("remote get module pins", remote)
		out, err := rmtSvc.GetModulePins(ctx, req)
		if err != nil {
			fmt.Println("remote get module pins error", err)
			continue
		}
		fmt.Println("remote get module pins success", out)
		resp.Msg.ModulePins = append(resp.Msg.ModulePins, out.Msg.ModulePins...)
	}

	return resp, nil
}

func (reg *Registry) handleLocalGetModulePins(ctx context.Context, req *connect.Request[registryv1alpha1.GetModulePinsRequest]) (*connect.Response[registryv1alpha1.GetModulePinsResponse], error) {
	out := &connect.Response[registryv1alpha1.GetModulePinsResponse]{
		Msg: &registryv1alpha1.GetModulePinsResponse{},
	}

	for _, m := range req.Msg.ModuleReferences {
		repo, err := reg.getRepository(ctx, m.Owner, m.Repository)
		if err != nil {
			fmt.Println("local get module pins error", err)
			return nil, err
		}
		_, mani, err := repo.FilesAndManifest(m.Reference)
		if err != nil {
			return nil, err
		}
		out.Msg.ModulePins = append(out.Msg.ModulePins, &modulev1alpha1.ModulePin{
			Remote:         m.Remote,
			Owner:          m.Owner,
			Repository:     m.Repository,
			Commit:         mani.Commit,
			ManifestDigest: "shake256:" + mani.SHAKE256,
		})
	}

	return out, nil
}

func requeststPerRemote(req *connect.Request[registryv1alpha1.GetModulePinsRequest]) map[string]*connect.Request[registryv1alpha1.GetModulePinsRequest] {
	reqPerRemote := map[string]*connect.Request[registryv1alpha1.GetModulePinsRequest]{}

	for _, m := range req.Msg.ModuleReferences {
		req, ok := reqPerRemote[m.Remote]
		if !ok {
			req = &connect.Request[registryv1alpha1.GetModulePinsRequest]{
				Msg: &registryv1alpha1.GetModulePinsRequest{
					ModuleReferences: []*modulev1alpha1.ModuleReference{},
				},
			}
			reqPerRemote[m.Remote] = req
		}
		req.Msg.ModuleReferences = append(req.Msg.ModuleReferences, m)
	}

	for _, m := range req.Msg.CurrentModulePins {
		req, ok := reqPerRemote[m.Remote]
		if !ok {
			req = &connect.Request[registryv1alpha1.GetModulePinsRequest]{
				Msg: &registryv1alpha1.GetModulePinsRequest{
					ModuleReferences: []*modulev1alpha1.ModuleReference{},
				},
			}
			reqPerRemote[m.Remote] = req
		}
		req.Msg.CurrentModulePins = append(req.Msg.CurrentModulePins, m)
	}

	return reqPerRemote
}

func (reg *Registry) GetGoVersion(ctx context.Context, req *connect.Request[registryv1alpha1.GetGoVersionRequest]) (*connect.Response[registryv1alpha1.GetGoVersionResponse], error) {
	return &connect.Response[registryv1alpha1.GetGoVersionResponse]{}, status.Errorf(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetSwiftVersion(ctx context.Context, req *connect.Request[registryv1alpha1.GetSwiftVersionRequest]) (*connect.Response[registryv1alpha1.GetSwiftVersionResponse], error) {
	return &connect.Response[registryv1alpha1.GetSwiftVersionResponse]{}, status.Errorf(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetMavenVersion(ctx context.Context, req *connect.Request[registryv1alpha1.GetMavenVersionRequest]) (*connect.Response[registryv1alpha1.GetMavenVersionResponse], error) {
	return &connect.Response[registryv1alpha1.GetMavenVersionResponse]{}, status.Errorf(codes.Unimplemented, "not implemented")
}

func (reg *Registry) GetNPMVersion(ctx context.Context, req *connect.Request[registryv1alpha1.GetNPMVersionRequest]) (*connect.Response[registryv1alpha1.GetNPMVersionResponse], error) {
	return &connect.Response[registryv1alpha1.GetNPMVersionResponse]{}, status.Errorf(codes.Unimplemented, "not implemented")
}
