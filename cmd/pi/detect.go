package main

import (
	"net/http"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/pi"
)

// loginDetectors returns the pi-CLI credential detectors: stored `pi login`
// credentials (~/.pigo/auth.json) first, then logins reused from the official
// provider CLIs. They register ahead of pkg/pi's environment detectors, so a
// deliberate `pi login` wins over ambient environment variables.
//
// The reuse-tier detection itself lives in the provider packages
// ([anthropic.DetectClaudeCLI], [openairesponses.DetectCodexCLI]); this only
// orders them. The auth.json store is pi-CLI-specific and stays here.
func loginDetectors() []pi.Detector {
	return append(authFileDetectors(), cliLoginDetectors()...)
}

// authFileDetectors builds detectors for the OAuth credentials stored by
// `pi login`, one per supported provider.
func authFileDetectors() []pi.Detector {
	fromFile := func(name string, build func(StoredCredential) catalog.Provider) pi.Detector {
		return pi.Detector{
			Name:   name,
			Source: "~/.pigo/auth.json",
			Detect: func() (catalog.Provider, bool) {
				stored, err := LoadAuth()
				if err != nil {
					return nil, false
				}
				sc, ok := stored[name]
				if !ok {
					return nil, false
				}
				return build(sc), true
			},
		}
	}
	return []pi.Detector{
		fromFile("anthropic", func(sc StoredCredential) catalog.Provider {
			return anthropic.New(anthropic.WithOAuth(
				sc.ClientID, sc.ToOAuthCredentials(),
				debugBase(), persistRefresh(sc),
			))
		}),
		fromFile("openai", func(sc StoredCredential) catalog.Provider {
			return openairesponses.NewForCodexOAuth(
				sc.ClientID, "", sc.ToOAuthCredentials(),
				debugBase(), persistRefresh(sc),
			)
		}),
	}
}

// cliLoginDetectors reuses a login already performed by an official provider
// CLI (Claude Code, Codex). The detection lives in the provider packages; here
// it is just wired into the chain with a source label.
func cliLoginDetectors() []pi.Detector {
	return []pi.Detector{
		{Name: "anthropic", Source: "Claude Code login", Detect: pi.ProviderDetector(anthropic.DetectClaudeCLI)},
		{Name: "openai", Source: "Codex login", Detect: pi.ProviderDetector(openairesponses.DetectCodexCLI)},
	}
}

// debugBase wraps the default transport with the optional verbose HTTP debug
// logger, as an [oauth.TransportOption] for the OAuth detectors.
func debugBase() oauth.TransportOption {
	return oauth.WithBase(maybeDebugTransport(http.DefaultTransport))
}

// persistRefresh returns an [oauth.TransportOption] that writes refreshed
// tokens back to auth.json.
func persistRefresh(sc StoredCredential) oauth.TransportOption {
	return oauth.WithOnRefresh(func(creds oauth.Credentials) error {
		stored, err := LoadAuth()
		if err != nil {
			return err
		}
		stored[findProviderName(sc.ClientID)] = FromOAuthCredentials(
			creds, sc.ClientID, sc.ClientSecret,
		)
		return SaveAuth(stored)
	})
}

// findProviderName returns the provider name for a given client ID by
// scanning the auth file.
func findProviderName(clientID string) string {
	stored, err := LoadAuth()
	if err != nil {
		return ""
	}
	for name, sc := range stored {
		if sc.ClientID == clientID {
			return name
		}
	}
	return ""
}
