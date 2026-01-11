package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry/cas"
)

func (svc *Service) Download(ctx context.Context, req *connect.Request[v1beta1.DownloadRequest]) (*connect.Response[v1beta1.DownloadResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	resp := &connect.Response[v1beta1.DownloadResponse]{
		Msg: &v1beta1.DownloadResponse{},
	}

	for _, ref := range req.Msg.Values {
		var commitId string

		switch ref := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			commitId = ref.Id
		case *v1beta1.ResourceRef_Name_:
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("ResourceRef_Name_ not supported"))
		}

		slog.DebugContext(ctx, "download commit", "commitId", commitId)

		content, err := svc.downloadByCommitID(ctx, commitId)
		if err != nil {
			return nil, err
		}

		resp.Msg.Contents = append(resp.Msg.Contents, content)
	}

	return resp, nil
}

func (svc *Service) downloadByCommitID(ctx context.Context, commitId string) (*v1beta1.DownloadResponse_Content, error) {
	mod, err := svc.casReg.ModuleByCommitID(ctx, commitId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitId))
	}

	slog.DebugContext(ctx, "download module", "module", mod.Name(), "owner", mod.Owner())

	files, commit, err := mod.FilesAndCommitByCommitID(ctx, commitId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	commitObj, err := getCommitObject(commit.OwnerID, commit.ModuleID, commit.ID, commit.ManifestDigest.Hex())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return buildDownloadContent(commitObj, files), nil
}

func buildDownloadContent(commit *v1beta1.Commit, files []cas.File) *v1beta1.DownloadResponse_Content {
	contents := &v1beta1.DownloadResponse_Content{
		Commit: commit,
	}

	for _, file := range files {
		if file.Path == "buf.yaml" {
			slog.Debug("buf.yaml found", "content", file.Content)
			contents.V1BufYamlFile = &v1beta1.File{
				Path:    file.Path,
				Content: []byte(file.Content),
			}
		}
		if file.Path == "buf.lock" {
			slog.Debug("buf.lock found", "content", file.Content)
			contents.V1BufLockFile = &v1beta1.File{
				Path:    file.Path,
				Content: []byte(file.Content),
			}
		}
		contents.Files = append(contents.Files, &v1beta1.File{
			Path:    file.Path,
			Content: []byte(file.Content),
		})
	}

	return contents
}
