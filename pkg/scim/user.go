package scim

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/elimity-com/scim"
	"github.com/elimity-com/scim/errors"
	"github.com/elimity-com/scim/optional"
)

func newUserResourceHandler() scim.ResourceHandler {
	return testResourceHandler{
		data: map[string]testData{},
	}
}

type testData struct {
	resourceAttributes scim.ResourceAttributes
	meta               map[string]string
}

// simple in-memory resource database.
type testResourceHandler struct {
	data map[string]testData
}

func (h testResourceHandler) Create(r *http.Request, attributes scim.ResourceAttributes) (scim.Resource, error) {
	// create unique identifier
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	id := fmt.Sprintf("%04d", rng.Intn(9999))

	// store resource
	h.data[id] = testData{
		resourceAttributes: attributes,
	}

	now := time.Now()

	// return stored resource
	return scim.Resource{
		ID:         id,
		ExternalID: h.externalID(attributes),
		Attributes: attributes,
		Meta: scim.Meta{
			Created:      &now,
			LastModified: &now,
			Version:      fmt.Sprintf("v%s", id),
		},
	}, nil
}

func (h testResourceHandler) Delete(r *http.Request, id string) error {
	// check if resource exists
	_, ok := h.data[id]
	if !ok {
		return errors.ScimErrorResourceNotFound(id)
	}

	// delete resource
	delete(h.data, id)

	return nil
}

func (h testResourceHandler) Get(r *http.Request, id string) (scim.Resource, error) {
	// check if resource exists
	data, ok := h.data[id]
	if !ok {
		return scim.Resource{}, errors.ScimErrorResourceNotFound(id)
	}

	created, _ := time.ParseInLocation(time.RFC3339, fmt.Sprintf("%v", data.meta["created"]), time.UTC)
	lastModified, _ := time.Parse(time.RFC3339, fmt.Sprintf("%v", data.meta["lastModified"]))

	// return resource with given identifier
	return scim.Resource{
		ID:         id,
		ExternalID: h.externalID(data.resourceAttributes),
		Attributes: data.resourceAttributes,
		Meta: scim.Meta{
			Created:      &created,
			LastModified: &lastModified,
			Version:      fmt.Sprintf("%v", data.meta["version"]),
		},
	}, nil
}

func (h testResourceHandler) GetAll(r *http.Request, params scim.ListRequestParams) (scim.Page, error) {
	if params.Count == 0 {
		return scim.Page{
			TotalResults: len(h.data),
		}, nil
	}

	resources := make([]scim.Resource, 0)
	i := 1

	for k, v := range h.data {
		if i > (params.StartIndex + params.Count - 1) {
			break
		}

		if i >= params.StartIndex {
			resources = append(resources, scim.Resource{
				ID:         k,
				ExternalID: h.externalID(v.resourceAttributes),
				Attributes: v.resourceAttributes,
			})
		}
		i++
	}

	return scim.Page{
		TotalResults: len(h.data),
		Resources:    resources,
	}, nil
}

func (h testResourceHandler) Patch(r *http.Request, id string, operations []scim.PatchOperation) (scim.Resource, error) {
	if h.shouldReturnNoContent(id, operations) {
		return scim.Resource{}, nil
	}

	for _, op := range operations {
		switch op.Op {
		case scim.PatchOperationAdd:
			if op.Path != nil {
				h.data[id].resourceAttributes[op.Path.String()] = op.Value
			} else {
				valueMap := op.Value.(map[string]interface{})
				for k, v := range valueMap {
					if arr, ok := h.data[id].resourceAttributes[k].([]interface{}); ok {
						arr = append(arr, v)
						h.data[id].resourceAttributes[k] = arr
					} else {
						h.data[id].resourceAttributes[k] = v
					}
				}
			}
		case scim.PatchOperationReplace:
			if op.Path != nil {
				h.data[id].resourceAttributes[op.Path.String()] = op.Value
			} else {
				valueMap := op.Value.(map[string]interface{})
				for k, v := range valueMap {
					h.data[id].resourceAttributes[k] = v
				}
			}
		case scim.PatchOperationRemove:
			h.data[id].resourceAttributes[op.Path.String()] = nil
		}
	}

	created, _ := time.ParseInLocation(time.RFC3339, fmt.Sprintf("%v", h.data[id].meta["created"]), time.UTC)
	now := time.Now()

	// return resource with replaced attributes
	return scim.Resource{
		ID:         id,
		ExternalID: h.externalID(h.data[id].resourceAttributes),
		Attributes: h.data[id].resourceAttributes,
		Meta: scim.Meta{
			Created:      &created,
			LastModified: &now,
			Version:      fmt.Sprintf("%s.patch", h.data[id].meta["version"]),
		},
	}, nil
}

func (h testResourceHandler) Replace(r *http.Request, id string, attributes scim.ResourceAttributes) (scim.Resource, error) {
	// check if resource exists
	_, ok := h.data[id]
	if !ok {
		return scim.Resource{}, errors.ScimErrorResourceNotFound(id)
	}

	// replace (all) attributes
	h.data[id] = testData{
		resourceAttributes: attributes,
	}

	// return resource with replaced attributes
	return scim.Resource{
		ID:         id,
		ExternalID: h.externalID(attributes),
		Attributes: attributes,
	}, nil
}

func (h testResourceHandler) externalID(attributes scim.ResourceAttributes) optional.String {
	if eID, ok := attributes["externalId"]; ok {
		externalID, ok := eID.(string)
		if !ok {
			return optional.String{}
		}
		return optional.NewString(externalID)
	}

	return optional.String{}
}

func (h testResourceHandler) noContentOperation(id string, op scim.PatchOperation) bool {
	isRemoveOp := strings.EqualFold(op.Op, scim.PatchOperationRemove)

	dataValue, ok := h.data[id]
	if !ok {
		return isRemoveOp
	}
	var path string
	if op.Path != nil {
		path = op.Path.String()
	}
	attrValue, ok := dataValue.resourceAttributes[path]
	if ok && attrValue == op.Value {
		return true
	}
	if !ok && isRemoveOp {
		return true
	}

	switch opValue := op.Value.(type) {
	case map[string]interface{}:
		for k, v := range opValue {
			if v == dataValue.resourceAttributes[k] {
				return true
			}
		}

	case []map[string]interface{}:
		for _, m := range opValue {
			for k, v := range m {
				if v == dataValue.resourceAttributes[k] {
					return true
				}
			}
		}
	}
	return false
}

func (h testResourceHandler) shouldReturnNoContent(id string, ops []scim.PatchOperation) bool {
	for _, op := range ops {
		if h.noContentOperation(id, op) {
			continue
		}
		return false
	}
	return true
}
