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

// Upload implements the UploadService.Upload method.
// This is the main entry point for pushing modules to the registry.
func (svc *Service) Upload(
	ctx context.Context,
	req *connect.Request[v1beta1.UploadRequest],
) (*connect.Response[v1beta1.UploadResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	// Extract dependency commit IDs from DepRefs
	depCommitIDs := make([]string, 0, len(req.Msg.DepRefs))
	for _, ref := range req.Msg.DepRefs {
		depCommitIDs = append(depCommitIDs, ref.CommitId)
	}

	slog.DebugContext(ctx, "Upload", "contents", len(req.Msg.Contents), "depCommitIds", len(depCommitIDs))

	resp := &v1beta1.UploadResponse{
		Commits: make([]*v1beta1.Commit, 0, len(req.Msg.Contents)),
	}

	for _, content := range req.Msg.Contents {
		commit, err := svc.uploadContent(ctx, content, depCommitIDs)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.Commits = append(resp.Commits, commit)
	}

	return connect.NewResponse(resp), nil
}

func (svc *Service) uploadContent(ctx context.Context, content *v1beta1.UploadRequest_Content, depCommitIDs []string) (*v1beta1.Commit, error) {
	// Resolve module reference
	owner, modName, err := svc.resolveModuleRef(content.ModuleRef)
	if err != nil {
		return nil, fmt.Errorf("invalid module reference: %w", err)
	}

	slog.DebugContext(ctx, "uploading content", "owner", owner, "module", modName, "files", len(content.Files), "depCommitIds", len(depCommitIDs))

	// Get or create module
	mod, err := svc.casReg.GetOrCreateModule(ctx, owner, modName)
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
	labels := svc.extractLabels(content.ScopedLabelRefs)
	if len(labels) == 0 {
		// Default to "main" if no labels specified
		labels = []string{"main"}
	}

	// Create commit with dependency commit IDs
	commit, err := mod.CreateCommit(ctx, files, labels, content.SourceControlUrl, depCommitIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create commit: %w", err)
	}

	// Build response commit
	return svc.commitToProto(commit)
}

func (svc *Service) resolveModuleRef(ref *v1beta1.ModuleRef) (owner, name string, err error) {
	if ref == nil {
		return "", "", errors.New("module reference is required")
	}

	switch v := ref.Value.(type) {
	case *v1beta1.ModuleRef_Id:
		// Look up module by ID
		if svc.casReg == nil {
			return "", "", errors.New("CAS storage not configured")
		}
		mod, err := svc.casReg.ModuleByID(context.Background(), v.Id)
		if err != nil {
			return "", "", fmt.Errorf("module not found: %w", err)
		}
		return mod.Owner(), mod.Name(), nil

	case *v1beta1.ModuleRef_Name_:
		if v.Name == nil {
			return "", "", errors.New("module name is required")
		}
		return v.Name.Owner, v.Name.Module, nil

	default:
		return "", "", errors.New("unknown module reference type")
	}
}

func (svc *Service) extractLabels(refs []*v1beta1.ScopedLabelRef) []string {
	labels := make([]string, 0, len(refs))
	for _, ref := range refs {
		switch v := ref.Value.(type) {
		case *v1beta1.ScopedLabelRef_Name:
			labels = append(labels, v.Name)
		}
	}
	return labels
}

func (svc *Service) commitToProto(commit *cas.Commit) (*v1beta1.Commit, error) {
	return getCommitObject(
		commit.OwnerID,
		commit.ModuleID,
		commit.ID,
		commit.ManifestDigest.Hex(),
	)
}
