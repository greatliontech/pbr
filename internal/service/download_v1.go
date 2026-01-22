package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry"
)

// DownloadServiceV1 implements the v1 DownloadService interface by wrapping Service.
type DownloadServiceV1 struct {
	svc *Service
}

// NewDownloadServiceV1 creates a new v1 DownloadService wrapper.
func NewDownloadServiceV1(svc *Service) *DownloadServiceV1 {
	return &DownloadServiceV1{svc: svc}
}

// Download downloads content for the given resource references.
// This v1 endpoint returns commits with B5 digests (instead of B4 in v1beta1).
func (d *DownloadServiceV1) Download(ctx context.Context, req *connect.Request[v1.DownloadRequest]) (*connect.Response[v1.DownloadResponse], error) {
	if d.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	resp := &connect.Response[v1.DownloadResponse]{
		Msg: &v1.DownloadResponse{},
	}

	for _, value := range req.Msg.Values {
		var commitId string
		var owner, modName string

		ref := value.ResourceRef
		if ref == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("resource_ref is required"))
		}

		switch r := ref.Value.(type) {
		case *v1.ResourceRef_Id:
			commitId = r.Id
		case *v1.ResourceRef_Name_:
			if r.Name == nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
			}
			owner = r.Name.Owner
			modName = r.Name.Module

			mod, err := d.svc.casReg.Module(ctx, owner, modName)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
			}

			var cmt *registry.Commit
			switch child := r.Name.Child.(type) {
			case *v1.ResourceRef_Name_LabelName:
				cmt, err = mod.Commit(ctx, child.LabelName)
				if err != nil {
					return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("label not found: %s", child.LabelName))
				}
			case *v1.ResourceRef_Name_Ref:
				// Ref could be a commit ID or a label name - try commit ID first
				cmt, err = mod.CommitByID(ctx, child.Ref)
				if err != nil {
					// Try as a label
					cmt, err = mod.Commit(ctx, child.Ref)
					if err != nil {
						return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref not found: %s", child.Ref))
					}
				}
			default:
				// No child specified - use default label (main)
				cmt, err = mod.Commit(ctx, "main")
				if err != nil {
					return nil, connect.NewError(connect.CodeNotFound, errors.New("default label 'main' not found"))
				}
			}
			commitId = cmt.ID
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported resource_ref type"))
		}

		slog.DebugContext(ctx, "download commit v1", "commitId", commitId)

		content, err := d.downloadByCommitIDV1(ctx, commitId, value.FileTypes, value.Paths, value.PathsAllowNotExist)
		if err != nil {
			return nil, err
		}

		resp.Msg.Contents = append(resp.Msg.Contents, content)
	}

	return resp, nil
}

func (d *DownloadServiceV1) downloadByCommitIDV1(ctx context.Context, commitId string, fileTypes []v1.FileType, paths []string, pathsAllowNotExist bool) (*v1.DownloadResponse_Content, error) {
	mod, err := d.svc.casReg.ModuleByCommitID(ctx, commitId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitId))
	}

	slog.DebugContext(ctx, "download module v1", "module", mod.Name(), "owner", mod.Owner())

	files, commit, err := mod.FilesAndCommitByCommitID(ctx, commitId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	commitObj := getCommitObjectV1(commit)
	return buildDownloadContentV1(commitObj, files, fileTypes, paths, pathsAllowNotExist), nil
}

func buildDownloadContentV1(commit *v1.Commit, files []registry.File, fileTypes []v1.FileType, paths []string, pathsAllowNotExist bool) *v1.DownloadResponse_Content {
	contents := &v1.DownloadResponse_Content{
		Commit: commit,
	}

	// Filter files based on fileTypes and paths if specified
	for _, file := range files {
		// Check path filter
		if len(paths) > 0 && !matchPath(file.Path, paths) {
			continue
		}

		// Check file type filter
		if len(fileTypes) > 0 && !matchFileType(file.Path, fileTypes) {
			continue
		}

		contents.Files = append(contents.Files, &v1.File{
			Path:    file.Path,
			Content: []byte(file.Content),
		})
	}

	return contents
}

// matchPath checks if a file path matches any of the specified paths.
// paths can be exact matches or directory prefixes.
func matchPath(filePath string, paths []string) bool {
	for _, p := range paths {
		if filePath == p {
			return true
		}
		// Check if path is a directory prefix
		if len(filePath) > len(p) && filePath[:len(p)] == p && filePath[len(p)] == '/' {
			return true
		}
	}
	return false
}

// matchFileType checks if a file matches any of the specified file types.
func matchFileType(filePath string, fileTypes []v1.FileType) bool {
	for _, ft := range fileTypes {
		switch ft {
		case v1.FileType_FILE_TYPE_PROTO:
			if len(filePath) > 6 && filePath[len(filePath)-6:] == ".proto" {
				return true
			}
		case v1.FileType_FILE_TYPE_LICENSE:
			if filePath == "LICENSE" || filePath == "LICENSE.txt" || filePath == "LICENSE.md" {
				return true
			}
		case v1.FileType_FILE_TYPE_DOC:
			if filePath == "README.md" || filePath == "README" || filePath == "buf.md" {
				return true
			}
		case v1.FileType_FILE_TYPE_UNSPECIFIED:
			// Match all files
			return true
		}
	}
	return false
}
