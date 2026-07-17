package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// ObjectProvider is an optional capability interface for providers that
// support structured object generation via JSON Schema.
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

// ObjectResult is the generic typed result returned by [GenerateObject].
type ObjectResult[T any] struct {
	Object T
	Raw    string
	Usage  Usage
}

// ObjectModel is the object-generation upgrade of a [LanguageModel].
// [GenerateObject] upgrades to it at runtime. The default binding from
// [NewLanguageModel] implements it when the bound provider is an
// [ObjectProvider]; wrappers around a LanguageModel (middleware,
// decorators) should implement and forward it to keep object support.
type ObjectModel interface {
	GenerateObject(ctx context.Context, p Prompt, schema *jsonschema.Schema, opts StreamOptions) (*ObjectResponse, error)
}

// GenerateObject implements [ObjectModel] by delegating to the bound
// provider. It errors when the provider is not an [ObjectProvider].
func (m languageModel) GenerateObject(
	ctx context.Context,
	p Prompt,
	schema *jsonschema.Schema,
	opts StreamOptions,
) (*ObjectResponse, error) {
	op, ok := m.prov.(ObjectProvider)
	if !ok {
		return nil, errors.New("ai: model's provider does not support object generation")
	}
	return op.GenerateObject(ctx, m.info, p, schema, opts)
}

// GenerateObject generates a typed object from lm. It requires lm's bound
// provider to implement [ObjectProvider]; otherwise it returns an error.
// T must be JSON-deserializable.
func GenerateObject[T any](
	ctx context.Context,
	lm LanguageModel,
	p Prompt,
	opts ...Option,
) (*ObjectResult[T], error) {
	om, ok := lm.(ObjectModel)
	if !ok {
		return nil, errors.New("ai: model does not support object generation")
	}

	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("ai: failed to generate schema: %w", err)
	}

	resp, err := om.GenerateObject(ctx, p, schema, ApplyOptions(opts))
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
