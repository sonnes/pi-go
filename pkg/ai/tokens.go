package ai

import (
	"context"
	"errors"
)

// TokenCounter is an optional capability interface for providers that can
// count a prompt's input tokens before generating a response.
type TokenCounter interface {
	CountTokens(
		ctx context.Context,
		model Model,
		p Prompt,
		opts StreamOptions,
	) (*TokenCount, error)
}

// TokenCount is the provider's count of tokens in a request.
type TokenCount struct {
	Total int
}

// TokenCountModel is the token-counting upgrade of a [LanguageModel].
// [CountTokens] upgrades to it at runtime. Wrappers around a LanguageModel
// should implement and forward it to keep token counting available.
type TokenCountModel interface {
	CountTokens(
		ctx context.Context,
		p Prompt,
		opts StreamOptions,
	) (*TokenCount, error)
}

// CountTokens implements [TokenCountModel] by delegating to the bound
// provider. It errors when the provider is not a [TokenCounter].
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

// CountTokens counts the input tokens in p using lm. It requires lm's bound
// provider to implement [TokenCounter]; otherwise it returns an error.
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
