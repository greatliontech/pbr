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

// UploadServiceV1 implements the v1 UploadService interface by wrapping Service.
type UploadServiceV1 struct {
	svc *Service
}

// NewUploadServiceV1 creates a new v1 UploadService wrapper.
func NewUploadServiceV1(svc *Service) *UploadServiceV1 {
	return &UploadServiceV1{svc: svc}
}

// Upload implements the v1 UploadService.Upload method.
func (u *UploadServiceV1) Upload(
	ctx context.Context,
	req *connect.Request[v1.UploadRequest],
) (*connect.Response[v1.UploadResponse], error) {
	if u.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "UploadV1", "contents", len(req.Msg.Contents))

	resp := &v1.UploadResponse{
		Commits: make([]*v1.Commit, 0, len(req.Msg.Contents)),
	}

	for _, content := range req.Msg.Contents {
		commit, err := u.uploadContent(ctx, content)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.Commits = append(resp.Commits, commit)
	}

	return connect.NewResponse(resp), nil
}

func (u *UploadServiceV1) uploadContent(ctx context.Context, content *v1.UploadRequest_Content) (*v1.Commit, error) {
	// Resolve module reference
	owner, modName, err := u.resolveModuleRef(content.ModuleRef)
	if err != nil {
		return nil, fmt.Errorf("invalid module reference: %w", err)
	}

	slog.DebugContext(ctx, "uploading content v1", "owner", owner, "module", modName, "files", len(content.Files))

	// Get or create module
	mod, err := u.svc.casReg.GetOrCreateModule(ctx, owner, modName)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create module: %w", err)
	}

	// Convert files
	files := make([]cas.File, 0, len(content.Files))
	for _, f := range content.Files {
		files = append(files, cas.File{
			Path:    f.Path,
			Content: string(f.Content),
		})
	}

	// Extract labels from scoped label refs
	labels := u.extractLabels(content.ScopedLabelRefs)
	if len(labels) == 0 {
		// Default to "main" if no labels specified
		labels = []string{"main"}
	}

	// Create commit
	commit, err := mod.CreateCommit(ctx, files, labels, content.SourceControlUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to create commit: %w", err)
	}

	// Build response commit
	return u.commitToProto(commit)
}

func (u *UploadServiceV1) resolveModuleRef(ref *v1.ModuleRef) (owner, name string, err error) {
	if ref == nil {
		return "", "", errors.New("module reference is required")
	}

	switch v := ref.Value.(type) {
	case *v1.ModuleRef_Id:
		// Look up module by ID
		if u.svc.casReg == nil {
			return "", "", errors.New("CAS storage not configured")
		}
		mod, err := u.svc.casReg.ModuleByID(context.Background(), v.Id)
		if err != nil {
			return "", "", fmt.Errorf("module not found: %w", err)
		}
		return mod.Owner(), mod.Name(), nil

	case *v1.ModuleRef_Name_:
		if v.Name == nil {
			return "", "", errors.New("module name is required")
		}
		return v.Name.Owner, v.Name.Module, nil

	default:
		return "", "", errors.New("unknown module reference type")
	}
}

func (u *UploadServiceV1) extractLabels(refs []*v1.ScopedLabelRef) []string {
	labels := make([]string, 0, len(refs))
	for _, ref := range refs {
		switch v := ref.Value.(type) {
		case *v1.ScopedLabelRef_Name:
			labels = append(labels, v.Name)
		}
	}
	return labels
}

func (u *UploadServiceV1) commitToProto(commit *cas.Commit) (*v1.Commit, error) {
	digest, err := hex.DecodeString(commit.ManifestDigest.Hex())
	if err != nil {
		return nil, fmt.Errorf("failed to decode digest: %w", err)
	}

	return &v1.Commit{
		Id:         commit.ID,
		OwnerId:    commit.OwnerID,
		ModuleId:   commit.ModuleID,
		CreateTime: timestamppb.New(commit.CreateTime),
		Digest: &v1.Digest{
			Type:  v1.DigestType_DIGEST_TYPE_B5,
			Value: digest,
		},
	}, nil
}
