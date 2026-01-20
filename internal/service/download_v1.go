package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry/cas"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DownloadServiceV1 implements the v1 DownloadService interface by wrapping Service.
type DownloadServiceV1 struct {
	svc *Service
}

// NewDownloadServiceV1 creates a new v1 DownloadService wrapper.
func NewDownloadServiceV1(svc *Service) *DownloadServiceV1 {
	return &DownloadServiceV1{svc: svc}
}

// Download implements the v1 DownloadService.Download method.
func (d *DownloadServiceV1) Download(
	ctx context.Context,
	req *connect.Request[v1.DownloadRequest],
) (*connect.Response[v1.DownloadResponse], error) {
	if d.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "DownloadV1", "values", len(req.Msg.Values))

	resp := &v1.DownloadResponse{
		Contents: make([]*v1.DownloadResponse_Content, 0, len(req.Msg.Values)),
	}

	for _, val := range req.Msg.Values {
		content, err := d.downloadValue(ctx, val)
		if err != nil {
			return nil, err
		}
		resp.Contents = append(resp.Contents, content)
	}

	return connect.NewResponse(resp), nil
}

func (d *DownloadServiceV1) downloadValue(ctx context.Context, val *v1.DownloadRequest_Value) (*v1.DownloadResponse_Content, error) {
	ref := val.ResourceRef
	if ref == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("resource_ref is required"))
	}

	switch r := ref.Value.(type) {
	case *v1.ResourceRef_Id:
		return d.downloadByCommitID(ctx, r.Id)
	case *v1.ResourceRef_Name_:
		return d.downloadByName(ctx, r.Name)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown resource ref type"))
	}
}

func (d *DownloadServiceV1) downloadByCommitID(ctx context.Context, commitID string) (*v1.DownloadResponse_Content, error) {
	slog.DebugContext(ctx, "downloadByCommitID v1", "commitID", commitID)

	mod, err := d.svc.casReg.ModuleByCommitID(ctx, commitID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", commitID))
	}

	files, commit, err := mod.FilesAndCommitByCommitID(ctx, commitID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return d.buildContent(commit, files)
}

func (d *DownloadServiceV1) downloadByName(ctx context.Context, name *v1.ResourceRef_Name) (*v1.DownloadResponse_Content, error) {
	if name == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	owner := name.Owner
	modName := name.Module

	slog.DebugContext(ctx, "downloadByName v1", "owner", owner, "module", modName)

	mod, err := d.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found: %s/%s", owner, modName))
	}

	// Determine the ref (label name or commit ref)
	var ref string
	switch child := name.Child.(type) {
	case *v1.ResourceRef_Name_LabelName:
		ref = child.LabelName
	case *v1.ResourceRef_Name_Ref:
		ref = child.Ref
	default:
		// No child specified - use default label (main)
		ref = "main"
	}

	files, commit, err := mod.FilesAndCommit(ctx, ref)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref not found: %s", ref))
	}

	return d.buildContent(commit, files)
}

func (d *DownloadServiceV1) buildContent(commit *cas.Commit, files []cas.File) (*v1.DownloadResponse_Content, error) {
	digest, err := hex.DecodeString(commit.ManifestDigest.Hex())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to decode digest: %w", err))
	}

	commitProto := &v1.Commit{
		Id:         commit.ID,
		OwnerId:    commit.OwnerID,
		ModuleId:   commit.ModuleID,
		CreateTime: timestamppb.New(commit.CreateTime),
		Digest: &v1.Digest{
			Type:  v1.DigestType_DIGEST_TYPE_B5,
			Value: digest,
		},
	}

	content := &v1.DownloadResponse_Content{
		Commit: commitProto,
		Files:  make([]*v1.File, 0, len(files)),
	}

	for _, file := range files {
		content.Files = append(content.Files, &v1.File{
			Path:    file.Path,
			Content: []byte(file.Content),
		})
	}

	return content, nil
}
