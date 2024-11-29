package registry

import (
	"context"
	"fmt"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
)

func (reg *Registry) Download(ctx context.Context, req *connect.Request[v1beta1.DownloadRequest]) (*connect.Response[v1beta1.DownloadResponse], error) {
	resp := &connect.Response[v1beta1.DownloadResponse]{
		Msg: &v1beta1.DownloadResponse{},
	}

	for _, ref := range req.Msg.Values {
		refStr := ""
		var mod *internalModule
		var commit *v1beta1.Commit

		switch ref := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			refStr = reg.commitHashes[ref.Id]
			mod = reg.commitToModule[ref.Id]
			commit = reg.commits[ref.Id]
		case *v1beta1.ResourceRef_Name_:
			return nil, fmt.Errorf("ResourceRef_Name_ not supported")
		}

		repo, err := reg.getRepository(ctx, mod.Owner, mod.Module)
		if err != nil {
			return nil, err
		}

		files, _, err := repo.FilesAndManifest(refStr)
		if err != nil {
			return nil, err
		}

		contents := &v1beta1.DownloadResponse_Content{
			Commit: commit,
		}

		for _, file := range files {
			if file.Name == "buf.yaml" {
				contents.V1BufLockFile = &v1beta1.File{
					Path:    file.Name,
					Content: []byte(file.Content),
				}
			}
			contents.Files = append(contents.Files, &v1beta1.File{
				Path:    file.Name,
				Content: []byte(file.Content),
			})
		}

		resp.Msg.Contents = append(resp.Msg.Contents, contents)
	}

	return resp, nil
}
