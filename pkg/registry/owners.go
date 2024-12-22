package registry

import (
	"context"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/store"
)

// Get Users or Organizations by id or name.
func (reg *Registry) GetOwners(ctx context.Context, req *connect.Request[v1.GetOwnersRequest]) (*connect.Response[v1.GetOwnersResponse], error) {
	resp := connect.NewResponse(&v1.GetOwnersResponse{})
	var owner *store.Owner
	var err error
	for _, ownref := range req.Msg.OwnerRefs {
		switch ownref := ownref.Value.(type) {
		case *v1.OwnerRef_Id:
			slog.Debug("GetOwners by id", "id", ownref.Id)
			owner, err = reg.stor.GetOwner(ctx, ownref.Id)
			if err != nil {
				if err == store.ErrNotFound {
					return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("owner not found: %s", ownref.Id))
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get owner: %w", err))
			}
		case *v1.OwnerRef_Name:
			slog.Debug("GetOwners by name", "name", ownref.Name)
			owner, err = reg.stor.GetOwnerByName(ctx, ownref.Name)
			if err != nil {
				if err == store.ErrNotFound {
					return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("owner with name not found: %s", ownref.Name))
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get owner: %w", err))
			}
		}
	}
	resp.Msg.Owners = append(resp.Msg.Owners, &v1.Owner{
		Value: &v1.Owner_Organization{
			Organization: &v1.Organization{
				Id:   owner.ID,
				Name: owner.Name,
			},
		},
	})
	return resp, nil
}
