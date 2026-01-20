package service

import (
	"context"
	"errors"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
)

// LabelServiceV1 implements the v1 LabelService interface by wrapping Service.
type LabelServiceV1 struct {
	svc *Service
}

// NewLabelServiceV1 creates a new v1 LabelService wrapper.
func NewLabelServiceV1(svc *Service) *LabelServiceV1 {
	return &LabelServiceV1{svc: svc}
}

// GetLabels retrieves labels by reference.
func (l *LabelServiceV1) GetLabels(
	ctx context.Context,
	req *connect.Request[v1.GetLabelsRequest],
) (*connect.Response[v1.GetLabelsResponse], error) {
	if l.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "GetLabelsV1", "labelRefs", len(req.Msg.LabelRefs))

	resp := &v1.GetLabelsResponse{
		Labels: make([]*v1.Label, 0, len(req.Msg.LabelRefs)),
	}

	for _, ref := range req.Msg.LabelRefs {
		label, err := l.getLabelByRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		resp.Labels = append(resp.Labels, label)
	}

	return connect.NewResponse(resp), nil
}

// ListLabels lists labels for a given module.
func (l *LabelServiceV1) ListLabels(
	ctx context.Context,
	req *connect.Request[v1.ListLabelsRequest],
) (*connect.Response[v1.ListLabelsResponse], error) {
	if l.svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	slog.DebugContext(ctx, "ListLabelsV1", "resourceRef", req.Msg.ResourceRef)

	// Resolve resource reference
	resRef := req.Msg.ResourceRef
	if resRef == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("resource_ref is required"))
	}

	var owner, modName string
	switch r := resRef.Value.(type) {
	case *v1.ResourceRef_Name_:
		if r.Name == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
		}
		owner = r.Name.Owner
		modName = r.Name.Module
	case *v1.ResourceRef_Id:
		mod, err := l.svc.casReg.ModuleByID(ctx, r.Id)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		owner = mod.Owner()
		modName = mod.Name()
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported resource ref type"))
	}

	mod, err := l.svc.casReg.Module(ctx, owner, modName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	labels, err := mod.ListLabels(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp := &v1.ListLabelsResponse{}
	for _, label := range labels {
		resp.Labels = append(resp.Labels, &v1.Label{
			Id:       mod.ID() + "-" + label.Name,
			Name:     label.Name,
			OwnerId:  mod.OwnerID(),
			ModuleId: mod.ID(),
			CommitId: label.CommitID,
		})
	}

	return connect.NewResponse(resp), nil
}

// ListLabelHistory lists the history of a label.
func (l *LabelServiceV1) ListLabelHistory(
	ctx context.Context,
	req *connect.Request[v1.ListLabelHistoryRequest],
) (*connect.Response[v1.ListLabelHistoryResponse], error) {
	// Not implemented - return empty response
	return connect.NewResponse(&v1.ListLabelHistoryResponse{}), nil
}

// CreateOrUpdateLabels creates or updates labels.
func (l *LabelServiceV1) CreateOrUpdateLabels(
	ctx context.Context,
	req *connect.Request[v1.CreateOrUpdateLabelsRequest],
) (*connect.Response[v1.CreateOrUpdateLabelsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CreateOrUpdateLabels not implemented"))
}

// ArchiveLabels archives labels.
func (l *LabelServiceV1) ArchiveLabels(
	ctx context.Context,
	req *connect.Request[v1.ArchiveLabelsRequest],
) (*connect.Response[v1.ArchiveLabelsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("ArchiveLabels not implemented"))
}

// UnarchiveLabels unarchives labels.
func (l *LabelServiceV1) UnarchiveLabels(
	ctx context.Context,
	req *connect.Request[v1.UnarchiveLabelsRequest],
) (*connect.Response[v1.UnarchiveLabelsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("UnarchiveLabels not implemented"))
}

func (l *LabelServiceV1) getLabelByRef(ctx context.Context, ref *v1.LabelRef) (*v1.Label, error) {
	if ref == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("label_ref is required"))
	}

	switch r := ref.Value.(type) {
	case *v1.LabelRef_Name_:
		if r.Name == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
		}
		owner := r.Name.Owner
		modName := r.Name.Module
		labelName := r.Name.Label

		mod, err := l.svc.casReg.Module(ctx, owner, modName)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}

		// Get commit for this label
		commit, err := mod.Commit(ctx, labelName)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}

		return &v1.Label{
			Id:       mod.ID() + "-" + labelName,
			Name:     labelName,
			OwnerId:  mod.OwnerID(),
			ModuleId: mod.ID(),
			CommitId: commit.ID,
		}, nil

	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported label ref type"))
	}
}
