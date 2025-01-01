package registry

import (
	"context"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
	"github.com/greatliontech/pbr/internal/util"
)

// Get Users or Organizations by id or name.
func (reg *Registry) GetOwners(ctx context.Context, req *connect.Request[v1.GetOwnersRequest]) (*connect.Response[v1.GetOwnersResponse], error) {
	resp := connect.NewResponse(&v1.GetOwnersResponse{})
	var ownerName string
	var ownerId string
	for _, ownref := range req.Msg.OwnerRefs {
		switch ownref := ownref.Value.(type) {
		case *v1.OwnerRef_Id:
			slog.Debug("GetOwners by id", "id", ownref.Id)
			on, ok := reg.ownerIds[ownref.Id]
			if !ok {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("owner not found: %s", ownref.Id))
			}
			ownerName = on
		case *v1.OwnerRef_Name:
			slog.Debug("GetOwners by name", "name", ownref.Name)
			ownerName = ownref.Name
			ownerId = util.OwnerID(ownerName)
		}
	}
	resp.Msg.Owners = append(resp.Msg.Owners, &v1.Owner{
		Value: &v1.Owner_Organization{
			Organization: &v1.Organization{
				Id:   ownerId,
				Name: ownerName,
			},
		},
	})
	return resp, nil
}
