package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	v1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/owner/v1"
	"connectrpc.com/connect"
)

// Get Users or Organizations by id or name.
func (svc *Service) GetOwners(ctx context.Context, req *connect.Request[v1.GetOwnersRequest]) (*connect.Response[v1.GetOwnersResponse], error) {
	if svc.casReg == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("CAS storage not configured"))
	}

	resp := connect.NewResponse(&v1.GetOwnersResponse{})

	for _, ownref := range req.Msg.OwnerRefs {
		var owner *v1.Owner
		var err error

		switch ref := ownref.Value.(type) {
		case *v1.OwnerRef_Id:
			slog.Debug("GetOwners by id", "id", ref.Id)
			ownerRecord, err := svc.casReg.Owner(ctx, ref.Id)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("owner not found: %s", ref.Id))
			}
			owner = &v1.Owner{
				Value: &v1.Owner_Organization{
					Organization: &v1.Organization{
						Id:   ownerRecord.ID,
						Name: ownerRecord.Name,
					},
				},
			}
		case *v1.OwnerRef_Name:
			slog.Debug("GetOwners by name", "name", ref.Name)
			ownerRecord, err := svc.casReg.OwnerByName(ctx, ref.Name)
			if err != nil {
				// Owner might not exist in metadata yet, create a virtual one
				owner = &v1.Owner{
					Value: &v1.Owner_Organization{
						Organization: &v1.Organization{
							Name: ref.Name,
						},
					},
				}
			} else {
				owner = &v1.Owner{
					Value: &v1.Owner_Organization{
						Organization: &v1.Organization{
							Id:   ownerRecord.ID,
							Name: ownerRecord.Name,
						},
					},
				}
			}
		default:
			err = errors.New("unknown owner reference type")
		}

		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}

		resp.Msg.Owners = append(resp.Msg.Owners, owner)
	}

	return resp, nil
}
