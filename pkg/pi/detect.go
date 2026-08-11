package pi

import (
	"fmt"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"

	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

// Detector builds a text provider from one credential source. If its source
// is absent, Detect reports (nil, false). ProviderID and Models are the
// explicit catalog metadata. Name filters on a hint and appears in the logs.
// Source labels the origin of the credential.
type Detector struct {
	ProviderID string
	Name       string
	Source     string
	Models     []ai.Model
	Detect     func() (ai.TextProvider, bool)
}

// Detection describes a successful auto-detection. It holds the catalog
// identity of the registered provider, the detector, and the source.
type Detection struct {
	Provider string // catalog identity, for example "anthropic-messages"
	Name     string // detector name, for example "anthropic"
	Source   string // credential source, for example "ANTHROPIC_API_KEY"
}

// ProviderDetector adapts the Detect function of a provider package into a
// [Detector.Detect]. That function returns its own concrete type.
func ProviderDetector[T ai.TextProvider](fn func() (T, bool)) func() (ai.TextProvider, bool) {
	return func() (ai.TextProvider, bool) {
		p, ok := fn()
		if !ok {
			return nil, false
		}
		return p, true
	}
}

// detectors is the precedence-ordered detection chain. Each provider owns its
// own environment detection. See the Detect function in each provider
// package. An application prepends higher-priority sources, such as stored
// logins, with [AddDetector].
var detectors = []Detector{
	{
		ProviderID: anthropic.ID,
		Name:       "anthropic",
		Source:     "ANTHROPIC_API_KEY/OAUTH_TOKEN",
		Models:     anthropic.Models(),
		Detect:     ProviderDetector(anthropic.Detect),
	},
	{
		ProviderID: openairesponses.ID,
		Name:       "openrouter",
		Source:     "OPENROUTER_API_KEY",
		Models:     openairesponses.Models(),
		Detect:     ProviderDetector(openairesponses.DetectOpenRouter),
	},
	{
		ProviderID: openairesponses.ID,
		Name:       "openai",
		Source:     "OPENAI_OAUTH_TOKEN",
		Models:     openairesponses.Models(),
		Detect:     ProviderDetector(openairesponses.DetectOAuthEnv),
	},
	{
		ProviderID: openai.ID,
		Name:       "openai",
		Source:     "OPENAI_API_KEY",
		Models:     openai.Models(),
		Detect:     ProviderDetector(openai.Detect),
	},
	{
		ProviderID: google.ID,
		Name:       "google",
		Source:     "GOOGLE_API_KEY",
		Models:     google.Models(),
		Detect:     ProviderDetector(google.Detect),
	},
}

func registerDetected(c *catalog.Catalog, d Detector, p ai.TextProvider) {
	c.RegisterTextProvider(d.ProviderID, p, d.Models...)
	if imageProvider, ok := p.(ai.ImageProvider); ok {
		c.RegisterImageProvider(d.ProviderID, imageProvider)
	}
	if speechProvider, ok := p.(ai.SpeechProvider); ok {
		c.RegisterSpeechProvider(d.ProviderID, speechProvider)
	}
}

// AddDetector prepends detectors to the default chain. They then take
// priority over the built-in environment detectors. Call AddDetector before
// the first resolution, so the added sources join the auto-wiring.
func AddDetector(d ...Detector) {
	detectors = append(append([]Detector(nil), d...), detectors...)
}

// Detect finds the first available API provider in the detection chain. The
// hint filters the chain on the provider Name. Detect registers the provider
// in [Default] and returns the detection. If no source yields credentials,
// Detect returns an error.
func Detect(hint string) (Detection, error) {
	for _, d := range detectors {
		if hint != "" && d.Name != hint {
			continue
		}
		p, ok := d.Detect()
		if !ok {
			continue
		}
		registerDetected(Default, d, p)
		return Detection{Provider: d.ProviderID, Name: d.Name, Source: d.Source}, nil
	}
	if hint != "" {
		return Detection{}, fmt.Errorf("no credentials found for provider %q", hint)
	}
	return Detection{}, fmt.Errorf(
		"no credentials found; set an API key (ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, …) or run `pi login`",
	)
}
