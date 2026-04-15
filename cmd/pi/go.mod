module github.com/sonnes/pi-go/cmd/pi

go 1.25.0

require (
	github.com/openai/openai-go v1.12.0
	github.com/sonnes/pi-go v0.0.0
	github.com/sonnes/pi-go/pkg/agent/claude v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/anthropic v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/claudecli v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/geminicli v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/google v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/openai v0.0.0
	github.com/stretchr/testify v1.11.1
	github.com/urfave/cli/v3 v3.3.3
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.36.0 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/lipgloss v1.1.0 // indirect
	github.com/charmbracelet/log v0.4.2 // indirect
	github.com/charmbracelet/x/ansi v0.8.0 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13-0.20250311204145-2c3ea96c31dd // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.40.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genai v1.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/grpc v1.66.2 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/sonnes/pi-go => ../..
	github.com/sonnes/pi-go/pkg/agent/claude => ../../pkg/agent/claude
	github.com/sonnes/pi-go/pkg/ai/provider/anthropic => ../../pkg/ai/provider/anthropic
	github.com/sonnes/pi-go/pkg/ai/provider/claudecli => ../../pkg/ai/provider/claudecli
	github.com/sonnes/pi-go/pkg/ai/provider/geminicli => ../../pkg/ai/provider/geminicli
	github.com/sonnes/pi-go/pkg/ai/provider/google => ../../pkg/ai/provider/google
	github.com/sonnes/pi-go/pkg/ai/provider/openai => ../../pkg/ai/provider/openai
)
