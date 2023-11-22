package registry

import (
	"context"
	"fmt"
	"strings"

	imagev1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/image/v1"
	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/pkg/codegen"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func (reg *Registry) GenerateCode(ctx context.Context, req *connect.Request[registryv1alpha1.GenerateCodeRequest]) (*connect.Response[registryv1alpha1.GenerateCodeResponse], error) {
	genReq := &pluginpb.CodeGeneratorRequest{}

	for _, f := range req.Msg.Image.File {
		// get buf extension
		ext := f.GetBufExtension()

		// add file to request
		genReq.ProtoFile = append(genReq.ProtoFile, toFileDescriptorProto(f))

		// mark file for generation
		if ext != nil && ext.IsImport != nil && !ext.GetIsImport() {
			name := f.GetName()
			genReq.FileToGenerate = append(genReq.FileToGenerate, name)
		}
	}

	resp := &connect.Response[registryv1alpha1.GenerateCodeResponse]{}
	resp.Msg = &registryv1alpha1.GenerateCodeResponse{}

	for _, request := range req.Msg.Requests {
		// join options
		opts := strings.Join(request.GetOptions(), ",")
		genReq.Parameter = &opts

		// prepare plugin
		plugin, err := reg.getPlugin(request.PluginReference)
		if err != nil {
			return nil, err
		}

		// run codegen
		out, err := plugin.CodeGen(genReq)
		if err != nil {
			return nil, err
		}

		resp.Msg.Responses = append(resp.Msg.Responses, &registryv1alpha1.PluginGenerationResponse{
			Response: out,
		})
	}

	return resp, nil
}

func (reg *Registry) getPlugin(ref *registryv1alpha1.CuratedPluginReference) (*codegen.Plugin, error) {
	name := ref.Owner + "/" + ref.Name
	plug, ok := reg.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin config not found: %s", name)
	}
	return plug, nil
}

func toFileDescriptorProto(f *imagev1.ImageFile) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:             f.Name,
		Package:          f.Package,
		Dependency:       f.Dependency,
		PublicDependency: f.PublicDependency,
		WeakDependency:   f.WeakDependency,
		MessageType:      f.MessageType,
		EnumType:         f.EnumType,
		Service:          f.Service,
		Extension:        f.Extension,
		Options:          f.Options,
		SourceCodeInfo:   f.SourceCodeInfo,
		Syntax:           f.Syntax,
		Edition:          f.Edition,
	}
}
