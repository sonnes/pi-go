package main

import (
	"net/http"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
	"github.com/sonnes/pi-go/pkg/pi"
)

// loginDetectors returns the pi-CLI credential detectors in order. The
// credentials that `pi login` stored (~/.pigo/auth.json) come first. The
// logins reused from the official provider CLIs come second. These detectors
// register ahead of the environment detectors of pkg/pi. A deliberate
// `pi login` therefore wins over ambient environment variables.
//
// The reuse-tier detection lives in the provider packages
// ([anthropic.DetectClaudeCLI], [openairesponses.DetectCodexCLI]). This
// function only orders them. The auth.json store belongs to the pi CLI
// alone, so it stays here.
func loginDetectors() []pi.Detector {
	return append(authFileDetectors(), cliLoginDetectors()...)
}

// authFileDetectors builds one detector for each supported provider. Each
// detector reads the OAuth credentials that `pi login` stored.
func authFileDetectors() []pi.Detector {
	fromFile := func(
		providerID string,
		name string,
		models []ai.Model,
		build func(name string, sc StoredCredential) ai.TextProvider,
	) pi.Detector {
		return pi.Detector{
			ProviderID: providerID,
			Name:       name,
			Source:     "~/.pigo/auth.json",
			Models:     models,
			Detect: func() (ai.TextProvider, bool) {
				stored, err := LoadAuth()
				if err != nil {
					return nil, false
				}
				sc, ok := stored[name]
				if !ok {
					return nil, false
				}
				return build(name, sc), true
			},
		}
	}
	return []pi.Detector{
		fromFile(anthropic.ID, "anthropic", anthropic.Models(), func(name string, sc StoredCredential) ai.TextProvider {
			return anthropic.New(anthropic.WithOAuth(
				sc.ClientID, sc.ToOAuthCredentials(),
				debugBase(), persistRefresh(name, sc),
			))
		}),
		fromFile(openairesponses.ID, "openai", openairesponses.Models(), func(name string, sc StoredCredential) ai.TextProvider {
			return openairesponses.NewForCodexOAuth(
				sc.ClientID, "", sc.ToOAuthCredentials(),
				debugBase(), persistRefresh(name, sc),
			)
		}),
	}
}

// cliLoginDetectors reuses a login from an official provider CLI, such as
// Claude Code or Codex. The detection lives in the provider packages. This
// function only adds it to the chain with a source label.
func cliLoginDetectors() []pi.Detector {
	return []pi.Detector{
		{
			ProviderID: anthropic.ID,
			Name:       "anthropic",
			Source:     "Claude Code login",
			Models:     anthropic.Models(),
			Detect:     pi.ProviderDetector(anthropic.DetectClaudeCLI),
		},
		{
			ProviderID: openairesponses.ID,
			Name:       "openai",
			Source:     "Codex login",
			Models:     openairesponses.Models(),
			Detect:     pi.ProviderDetector(openairesponses.DetectCodexCLI),
		},
	}
}

// debugBase returns an [oauth.TransportOption] for the OAuth detectors. It
// wraps the default transport with the optional verbose HTTP debug logger.
func debugBase() oauth.TransportOption {
	return oauth.WithBase(maybeDebugTransport(http.DefaultTransport))
}

// persistRefresh returns an [oauth.TransportOption] that writes refreshed
// tokens back to auth.json under the given provider name.
func persistRefresh(name string, sc StoredCredential) oauth.TransportOption {
	return oauth.WithOnRefresh(func(creds oauth.Credentials) error {
		stored, err := LoadAuth()
		if err != nil {
			return err
		}
		stored[name] = FromOAuthCredentials(creds, sc.ClientID, sc.ClientSecret)
		return SaveAuth(stored)
	})
}
