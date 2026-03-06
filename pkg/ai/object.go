package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// ObjectProvider is an optional interface for providers that support
// structured object generation via JSON Schema.
type ObjectProvider interface {
	GenerateObject(
		ctx context.Context,
		model Model,
		p Prompt,
		schema *jsonschema.Schema,
		opts StreamOptions,
	) (*ObjectResponse, error)
}

// ObjectResponse is the raw provider response for object generation.
type ObjectResponse struct {
	Raw   string
	Usage Usage
	Model string
}

// ObjectResult is the generic typed result returned by GenerateObject.
type ObjectResult[T any] struct {
	Object T
	Raw    string
	Usage  Usage
}

// GenerateObject generates a typed object. T must be JSON-deserializable.
func GenerateObject[T any](
	ctx context.Context,
	model Model,
	p Prompt,
	opts ...Option,
) (*ObjectResult[T], error) {
	prov, ok := GetProvider(model.API)
	if !ok {
		return nil, fmt.Errorf("ai: no provider registered for API %q", model.API)
	}
	op, ok := prov.(ObjectProvider)
	if !ok {
		return nil, fmt.Errorf(
			"ai: provider for API %q does not support object generation",
			model.API,
		)
	}

	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("ai: failed to generate schema: %w", err)
	}

	o := ApplyOptions(opts)
	resp, err := op.GenerateObject(ctx, model, p, schema, o)
	if err != nil {
		return nil, err
	}

	var obj T
	if err := json.Unmarshal([]byte(resp.Raw), &obj); err != nil {
		return nil, fmt.Errorf("ai: failed to unmarshal object: %w", err)
	}

	return &ObjectResult[T]{
		Object: obj,
		Raw:    resp.Raw,
		Usage:  resp.Usage,
	}, nil
}
