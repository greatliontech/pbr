package service

import (
	"context"
	"fmt"
	"log/slog"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
)

func (svc *Service) Download(ctx context.Context, req *connect.Request[v1beta1.DownloadRequest]) (*connect.Response[v1beta1.DownloadResponse], error) {
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

		slog.DebugContext(ctx, "downlowd commit", "commitId", commitId)

		modl, err := svc.reg.ModuleByCommitID(ctx, commitId)
		if err != nil {
			return nil, err
		}

		slog.DebugContext(ctx, "downlowd module", "module", modl.Name, "owner", modl.Owner)

		files, cmmt, err := modl.FilesAndCommitByCommitId(ctx, commitId)
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
				slog.DebugContext(ctx, "buf.yaml found", "content", file.Content)
				contents.V1BufYamlFile = &v1beta1.File{
					Path:    file.Name,
					Content: []byte(file.Content),
				}
			}
			if file.Name == "buf.lock" {
				slog.DebugContext(ctx, "buf.lock found", "content", file.Content)
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
