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
		var commitId string

		switch ref := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			commitId = ref.Id
		case *v1beta1.ResourceRef_Name_:
			return nil, fmt.Errorf("ResourceRef_Name_ not supported")
		}

		modl, err := reg.reg.ModuleByCommitID(ctx, commitId)
		if err != nil {
			return nil, err
		}

		files, cmmt, err := modl.FilesAndCommitByCommitId(commitId)
		if err != nil {
			return nil, err
		}

		commit, err := getCommitObject(cmmt.OwnerId, cmmt.ModuleId, cmmt.CommitId, cmmt.Digest)
		if err != nil {
			return nil, err
		}

		contents := &v1beta1.DownloadResponse_Content{
			Commit: commit,
		}

		for _, file := range files {
			if file.Name == "buf.yaml" {
				fmt.Println("buf.yaml found", file.Content)
				contents.V1BufYamlFile = &v1beta1.File{
					Path:    file.Name,
					Content: []byte(file.Content),
				}
			}
			if file.Name == "buf.lock" {
				fmt.Println("buf.lock found", file.Content)
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
