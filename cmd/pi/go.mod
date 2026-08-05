module github.com/sonnes/pi-go/cmd/pi

go 1.26.0

require (
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/glamour v1.0.0
	github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834
	github.com/charmbracelet/x/ansi v0.11.6
	github.com/charmbracelet/x/term v0.2.2
	github.com/sonnes/pi-go v0.0.0
	github.com/sonnes/pi-go/pkg/agent/claude v0.0.0
	github.com/sonnes/pi-go/pkg/agent/codex v0.0.0
	github.com/sonnes/pi-go/pkg/agent/cursor v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/anthropic v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/claudecli v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/codexcli v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/cursorcli v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/openai v0.0.0
	github.com/sonnes/pi-go/pkg/ai/provider/openairesponses v0.0.0
	github.com/sonnes/pi-go/pkg/pi v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.11.1
	github.com/urfave/cli/v3 v3.10.1
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/alecthomas/chroma/v2 v2.20.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.61.0 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/log v1.0.0 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/go-logfmt/logfmt v0.6.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/openai/openai-go/v3 v3.49.0 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sonnes/pi-go/pkg/ai/provider/google v0.0.0 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yuin/goldmark v1.7.13 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genai v1.66.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/grpc v1.66.2 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/sonnes/pi-go => ../..
	github.com/sonnes/pi-go/pkg/agent/claude => ../../pkg/agent/claude
	github.com/sonnes/pi-go/pkg/agent/codex => ../../pkg/agent/codex
	github.com/sonnes/pi-go/pkg/agent/cursor => ../../pkg/agent/cursor
	github.com/sonnes/pi-go/pkg/ai/provider/anthropic => ../../pkg/ai/provider/anthropic
	github.com/sonnes/pi-go/pkg/ai/provider/claudecli => ../../pkg/ai/provider/claudecli
	github.com/sonnes/pi-go/pkg/ai/provider/codexcli => ../../pkg/ai/provider/codexcli
	github.com/sonnes/pi-go/pkg/ai/provider/cursorcli => ../../pkg/ai/provider/cursorcli
	github.com/sonnes/pi-go/pkg/ai/provider/google => ../../pkg/ai/provider/google
	github.com/sonnes/pi-go/pkg/ai/provider/openai => ../../pkg/ai/provider/openai
	github.com/sonnes/pi-go/pkg/ai/provider/openairesponses => ../../pkg/ai/provider/openairesponses
	github.com/sonnes/pi-go/pkg/pi => ../../pkg/pi
)
