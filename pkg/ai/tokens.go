package ai

import (
	"context"
	"errors"
)

// TokenCounter is an optional capability interface. A provider implements it
// to count the input tokens of a prompt before it generates a response.
type TokenCounter interface {
	CountTokens(
		ctx context.Context,
		model Model,
		p Prompt,
		opts StreamOptions,
	) (*TokenCount, error)
}

// TokenCount is the number of tokens in a request, as counted by the
// provider.
type TokenCount struct {
	Total int
}

// TokenCountModel is the token-counting upgrade of a [LanguageModel].
// [CountTokens] upgrades to it at runtime. A wrapper around a LanguageModel
// must implement and forward it to keep token counting available.
type TokenCountModel interface {
	CountTokens(
		ctx context.Context,
		p Prompt,
		opts StreamOptions,
	) (*TokenCount, error)
}

// CountTokens implements [TokenCountModel]. It delegates to the bound
// provider. If the provider is not a [TokenCounter], it returns an error.
func (m languageModel) CountTokens(
	ctx context.Context,
	p Prompt,
	opts StreamOptions,
) (*TokenCount, error) {
	counter, ok := m.prov.(TokenCounter)
	if !ok {
		return nil, errors.New("ai: model's provider does not support token counting")
	}

	return counter.CountTokens(ctx, m.info, p, opts)
}

// CountTokens counts the input tokens in p with lm. The provider bound to lm
// must implement [TokenCounter]. If it does not, CountTokens returns an
// error.
func CountTokens(
	ctx context.Context,
	lm LanguageModel,
	p Prompt,
	opts ...Option,
) (*TokenCount, error) {
	counter, ok := lm.(TokenCountModel)
	if !ok {
		return nil, errors.New("ai: model does not support token counting")
	}

	return counter.CountTokens(ctx, p, ApplyOptions(opts))
}
