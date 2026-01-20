package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/registry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// protoImportRegex matches import statements in proto files.
var protoImportRegex = regexp.MustCompile(`import\s+"([^"]+)"`)

// extractProtoImports extracts import paths from proto files.
func extractProtoImports(files []registry.File) []string {
	imports := make([]string, 0)
	seen := make(map[string]bool)

	for _, f := range files {
		if len(f.Path) < 6 || f.Path[len(f.Path)-6:] != ".proto" {
			continue
		}

		matches := protoImportRegex.FindAllStringSubmatch(f.Content, -1)
		for _, match := range matches {
			if len(match) > 1 {
				imp := match[1]
				// Skip google protobuf imports (standard library)
				if len(imp) > 7 && imp[:7] == "google/" {
					continue
				}
				if !seen[imp] {
					seen[imp] = true
					imports = append(imports, imp)
				}
			}
		}
	}

	return imports
}

// UploadService implements the v1 UploadService interface by wrapping Service.
type UploadService struct {
	svc *Service
}

// NewUploadService creates a new v1 UploadService wrapper.
func NewUploadService(svc *Service) *UploadService {
	return &UploadService{svc: svc}
}

// Upload implements the v1 UploadService.Upload method.
func (u *UploadService) Upload(
	ctx context.Context,
	req *connect.Request[v1.UploadRequest],
) (*connect.Response[v1.UploadResponse], error) {
	if u.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "UploadV1", "contents", len(req.Msg.Contents), "depCommitIds", len(req.Msg.DepCommitIds))

	resp := &v1.UploadResponse{
		Commits: make([]*v1.Commit, 0, len(req.Msg.Contents)),
	}

	for _, content := range req.Msg.Contents {
		commit, err := u.uploadContent(ctx, content, req.Msg.DepCommitIds)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.Commits = append(resp.Commits, commit)
	}

	return connect.NewResponse(resp), nil
}

func (u *UploadService) uploadContent(ctx context.Context, content *v1.UploadRequest_Content, depCommitIDs []string) (*v1.Commit, error) {
	// Resolve module reference
	owner, modName, err := u.resolveModuleRef(content.ModuleRef)
	if err != nil {
		return nil, fmt.Errorf("invalid module reference: %w", err)
	}

	slog.DebugContext(ctx, "uploading content v1", "owner", owner, "module", modName, "files", len(content.Files), "depCommitIds", len(depCommitIDs))

	// Get or create module
	mod, err := u.svc.casReg.GetOrCreateModule(ctx, owner, modName)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create module: %w", err)
	}

	// Convert files
	files := make([]registry.File, 0, len(content.Files))
	for _, f := range content.Files {
		files = append(files, registry.File{
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

	// If no dependencies provided, try to detect from proto imports
	if len(depCommitIDs) == 0 {
		detectedDeps := u.detectDependenciesFromImports(ctx, files)
		if len(detectedDeps) > 0 {
			slog.DebugContext(ctx, "detected dependencies from imports", "count", len(detectedDeps))
			depCommitIDs = detectedDeps
		}
	}

	// Create commit with dependency commit IDs
	commit, err := mod.CreateCommit(ctx, files, labels, content.SourceControlUrl, depCommitIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create commit: %w", err)
	}

	// Build response commit
	return u.commitToProto(commit)
}

// detectDependenciesFromImports parses proto imports and tries to resolve them to known modules.
func (u *UploadService) detectDependenciesFromImports(ctx context.Context, files []registry.File) []string {
	imports := extractProtoImports(files)
	if len(imports) == 0 {
		return nil
	}

	slog.DebugContext(ctx, "extracted proto imports", "imports", imports)

	// Try to resolve each import to a known module
	depCommitIDs := make([]string, 0)
	seen := make(map[string]bool)

	for _, imp := range imports {
		// Look for modules that have this file
		commitID := u.findModuleWithFile(ctx, imp)
		if commitID != "" && !seen[commitID] {
			seen[commitID] = true
			depCommitIDs = append(depCommitIDs, commitID)
			slog.DebugContext(ctx, "resolved import to commit", "import", imp, "commitID", commitID)
		}
	}

	return depCommitIDs
}

// findModuleWithFile searches for a module that contains the given file path.
func (u *UploadService) findModuleWithFile(ctx context.Context, filePath string) string {
	// This is a simple implementation that searches all known modules.
	// In production, this could be optimized with an index.

	// List all owners and their modules
	owners, err := u.svc.casReg.ListOwners(ctx)
	if err != nil {
		slog.DebugContext(ctx, "failed to list owners", "error", err)
		return ""
	}

	for _, owner := range owners {
		modules, err := u.svc.casReg.ListModules(ctx, owner.Name)
		if err != nil {
			continue
		}

		for _, mod := range modules {
			// Get the latest commit (main label)
			commit, err := mod.Commit(ctx, "main")
			if err != nil {
				continue
			}

			// Check if this commit has the file
			files, _, err := mod.FilesAndCommitByCommitID(ctx, commit.ID)
			if err != nil {
				continue
			}

			for _, f := range files {
				if f.Path == filePath {
					return commit.ID
				}
			}
		}
	}

	return ""
}

func (u *UploadService) resolveModuleRef(ref *v1.ModuleRef) (owner, name string, err error) {
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

func (u *UploadService) extractLabels(refs []*v1.ScopedLabelRef) []string {
	labels := make([]string, 0, len(refs))
	for _, ref := range refs {
		switch v := ref.Value.(type) {
		case *v1.ScopedLabelRef_Name:
			labels = append(labels, v.Name)
		}
	}
	return labels
}

func (u *UploadService) commitToProto(commit *registry.Commit) (*v1.Commit, error) {
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
