package scim

import (
	"github.com/elimity-com/scim"
	"github.com/elimity-com/scim/optional"
	"github.com/elimity-com/scim/schema"
)

func NewServer() (scim.Server, error) {
	conf := &scim.ServiceProviderConfig{}
	args := &scim.ServerArgs{
		ServiceProviderConfig: conf,
	}
	s, err := scim.NewServer(args)
	if err != nil {
		return scim.Server{}, err
	}
	return s, nil
}

func resourceTypes() []scim.ResourceType {
	userSchema := schema.Schema{
		ID:          "urn:ietf:params:scim:schemas:core:2.0:User",
		Name:        optional.NewString("User"),
		Description: optional.NewString("User Account"),
		Attributes: []schema.CoreAttribute{
			schema.SimpleCoreAttribute(schema.SimpleStringParams(schema.StringParams{
				Name:       "userName",
				Required:   true,
				Uniqueness: schema.AttributeUniquenessServer(),
			})),
		},
	}

	return []scim.ResourceType{
		{
			ID:          optional.NewString("User"),
			Name:        "User",
			Endpoint:    "/Users",
			Description: optional.NewString("User Account"),
			Schema:      userSchema,
			Handler:     newUserResourceHandler(),
		},
	}
}
