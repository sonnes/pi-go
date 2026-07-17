package pi

import (
	"fmt"

	"github.com/sonnes/pi-go/pkg/catalog"

	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

// Detector builds a provider from one credential source. Detect reports
// (nil, false) when its source is absent. Name is the provider identity used
// for hint filtering and logging; Source labels where the credential came from
// (an env var, a login file, …).
type Detector struct {
	Name   string
	Source string
	Detect func() (catalog.Provider, bool)
}

// Detection describes a successful auto-detection: the catalog identity of the
// registered provider plus which detector and source produced it.
type Detection struct {
	Provider string // catalog identity, e.g. "anthropic-messages"
	Name     string // detector name, e.g. "anthropic"
	Source   string // credential source, e.g. "ANTHROPIC_API_KEY"
}

// ProviderDetector adapts a provider package's Detect function — which returns
// its own concrete type, so the provider modules stay free of the catalog
// dependency — into a [Detector.Detect].
func ProviderDetector[T catalog.Provider](fn func() (T, bool)) func() (catalog.Provider, bool) {
	return func() (catalog.Provider, bool) {
		p, ok := fn()
		if !ok {
			return nil, false
		}
		return p, true
	}
}

// detectors is the precedence-ordered detection chain. Each provider owns its
// own environment detection (see the provider package's Detect); applications
// prepend higher-priority sources — e.g. stored logins — with [AddDetector].
var detectors = []Detector{
	{"anthropic", "ANTHROPIC_API_KEY/OAUTH_TOKEN", ProviderDetector(anthropic.Detect)},
	{"openrouter", "OPENROUTER_API_KEY", ProviderDetector(openairesponses.DetectOpenRouter)},
	{"openai", "OPENAI_OAUTH_TOKEN", ProviderDetector(openairesponses.DetectOAuthEnv)},
	{"openai", "OPENAI_API_KEY", ProviderDetector(openai.Detect)},
	{"google", "GOOGLE_API_KEY", ProviderDetector(google.Detect)},
}

// AddDetector prepends detectors to the default chain, giving them priority
// over the built-in environment detectors. Call it before the first resolution
// so the added sources participate in auto-wiring.
func AddDetector(d ...Detector) {
	detectors = append(append([]Detector(nil), d...), detectors...)
}

// Detect finds the first available API provider in the detection chain
// (honoring hint, a provider Name filter), registers it in [Default], and
// returns the detection. It errors when no source yields credentials.
func Detect(hint string) (Detection, error) {
	for _, d := range detectors {
		if hint != "" && d.Name != hint {
			continue
		}
		p, ok := d.Detect()
		if !ok {
			continue
		}
		Default.RegisterProvider(p)
		return Detection{Provider: p.Provider(), Name: d.Name, Source: d.Source}, nil
	}
	if hint != "" {
		return Detection{}, fmt.Errorf("no credentials found for provider %q", hint)
	}
	return Detection{}, fmt.Errorf(
		"no credentials found; set an API key (ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, …) or run `pi login`",
	)
}
