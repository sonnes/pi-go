package claude

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// resultLine is the minimal CLI output that completes one turn.
const resultLine = `{"type":"result","subtype":"success","session_id":"s"}` + "\n"

// TestBuildUserLine_FoldsTurnIntoOneMessage is the constraint the CLI
// imposes: every user line on stdin starts its own turn. A turn that
// carries injected context plus the message of the user must therefore
// reach the CLI as one line, with the blocks in the order the caller
// wrote them.
func TestBuildUserLine_FoldsTurnIntoOneMessage(t *testing.T) {
	line, err := buildUserLine([]ai.Message{
		ai.UserMessage("<environment>cwd: /work</environment>"),
		ai.UserMessage("read mode is on"),
		ai.UserMessage("what does main.go do?"),
	})
	require.NoError(t, err)

	var got sdkUserMessage
	require.NoError(t, json.Unmarshal(line, &got))
	assert.Equal(t, "user", got.Type)
	assert.Equal(t, "user", got.Message.Role)

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(got.Message.Content, &blocks))
	require.Len(t, blocks, 3)
	assert.Equal(t, "<environment>cwd: /work</environment>", blocks[0]["text"])
	assert.Equal(t, "read mode is on", blocks[1]["text"])
	assert.Equal(t, "what does main.go do?", blocks[2]["text"])
}

// TestBuildUserLine_SingleTextStaysAString keeps the compact wire shape
// for the common one-message turn.
func TestBuildUserLine_SingleTextStaysAString(t *testing.T) {
	line, err := buildUserLine([]ai.Message{ai.UserMessage("hello")})
	require.NoError(t, err)

	var got sdkUserMessage
	require.NoError(t, json.Unmarshal(line, &got))

	var s string
	require.NoError(t, json.Unmarshal(got.Message.Content, &s))
	assert.Equal(t, "hello", s)
}

// TestBuildUserLine_KeepsImagesAcrossMessages checks that folding does
// not drop the richer blocks of an earlier message in the turn.
func TestBuildUserLine_KeepsImagesAcrossMessages(t *testing.T) {
	line, err := buildUserLine([]ai.Message{
		ai.UserImageMessage("look", ai.Image{Data: "AAA=", MimeType: "image/png"}),
		ai.UserMessage("what is it?"),
	})
	require.NoError(t, err)

	var got sdkUserMessage
	require.NoError(t, json.Unmarshal(line, &got))

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(got.Message.Content, &blocks))
	require.Len(t, blocks, 3)
	assert.Equal(t, "text", blocks[0]["type"])
	assert.Equal(t, "image", blocks[1]["type"])
	assert.Equal(t, "text", blocks[2]["type"])
}

// TestBuildUserLine_SkipsNonUserMessages checks that only the user turn
// reaches stdin. The CLI owns its own transcript, so an assistant
// message in the input is not ours to replay.
func TestBuildUserLine_SkipsNonUserMessages(t *testing.T) {
	line, err := buildUserLine([]ai.Message{
		{Role: ai.RoleAssistant, Content: []ai.Content{ai.Text{Text: "earlier reply"}}},
		ai.UserMessage("go on"),
	})
	require.NoError(t, err)

	var got sdkUserMessage
	require.NoError(t, json.Unmarshal(line, &got))

	var s string
	require.NoError(t, json.Unmarshal(got.Message.Content, &s))
	assert.Equal(t, "go on", s)
}

func TestBuildUserLine_NoUserMessage(t *testing.T) {
	_, err := buildUserLine([]ai.Message{
		{Role: ai.RoleAssistant, Content: []ai.Content{ai.Text{Text: "hi"}}},
	})
	require.ErrorContains(t, err, "no user text or image content")
}

// TestRun_WritesOneLineForTheWholeTurn is the same contract at the
// [Agent.Run] boundary, which is what durable calls.
func TestRun_WritesOneLineForTheWholeTurn(t *testing.T) {
	a, ft := newTestAgent(t, nil, emitString(resultLine))
	defer a.Close()

	_, err := a.Run(t.Context(),
		ai.UserMessage("injected context"),
		ai.UserMessage("the question"),
	).Wait()
	require.NoError(t, err)

	require.Equal(t, 1, ft.writeCount())
	assert.Contains(t, string(ft.writes()[0]), "injected context")
	assert.Contains(t, string(ft.writes()[0]), "the question")
}

// --- session identity ---

// TestBuildArgs_NewSessionCreates checks that a fresh session names its
// own ID. The CLI rejects --session-id for an ID it already has, so the
// create and the resume are different flags.
func TestBuildArgs_NewSessionCreates(t *testing.T) {
	cfg := config{
		cliPath:    "claude",
		sessionID:  "3f2504e0-4f89-41d3-9a0c-0305e82c3301",
		newSession: true,
	}
	args := strings.Join(buildArgs(cfg), " ")

	assert.Contains(t, args, "--session-id 3f2504e0-4f89-41d3-9a0c-0305e82c3301")
	assert.NotContains(t, args, "--resume")
}

func TestBuildArgs_ExistingSessionResumes(t *testing.T) {
	cfg := config{
		cliPath:   "claude",
		sessionID: "3f2504e0-4f89-41d3-9a0c-0305e82c3301",
	}
	args := strings.Join(buildArgs(cfg), " ")

	assert.Contains(t, args, "--resume 3f2504e0-4f89-41d3-9a0c-0305e82c3301")
	assert.NotContains(t, args, "--session-id")
}

// TestValidateSessionID rejects an ID the CLI cannot use. The CLI needs
// a UUID, and a caller whose session IDs have another shape must learn
// that before a subprocess starts rather than from its exit code.
func TestValidateSessionID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		ok   bool
	}{
		{"empty is fine", "", true},
		{"uuid", "3f2504e0-4f89-41d3-9a0c-0305e82c3301", true},
		{"uppercase uuid", "3F2504E0-4F89-41D3-9A0C-0305E82C3301", true},
		{"opaque name", "lw-session-42", false},
		{"hex without dashes", "9f3a1c7b2e4d6a80", false},
		{"too short", "3f2504e0-4f89-41d3-9a0c-0305e82c33", false},
		{"non-hex", "zzzzzzzz-4f89-41d3-9a0c-0305e82c3301", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSessionID(tt.id)
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "UUID")
		})
	}
}

// TestRun_RejectsNonUUIDSessionID checks that the bad ID fails the run
// with the explanation, and never starts a subprocess.
func TestRun_RejectsNonUUIDSessionID(t *testing.T) {
	a := New(ai.Model{}, WithSessionID("lw-session-42"))
	defer a.Close()

	var started bool
	a.newTransport = func(_ context.Context, _ config) (transportIface, error) {
		started = true
		return newFakeTransport(), nil
	}

	_, err := a.Run(t.Context(), ai.UserMessage("hi")).Wait()
	require.ErrorContains(t, err, "UUID")
	assert.False(t, started, "no subprocess should start")
}

// TestWithNewSession checks the option pair reaches the config.
func TestWithNewSession(t *testing.T) {
	id := "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

	cfg := agent.ApplyOptions(WithNewSession(id))
	ext, ok := cfg.Extensions[extensionKey].(*config)
	require.True(t, ok)
	assert.Equal(t, id, ext.sessionID)
	assert.True(t, ext.newSession)

	cfg = agent.ApplyOptions(WithSessionID(id))
	ext, ok = cfg.Extensions[extensionKey].(*config)
	require.True(t, ok)
	assert.Equal(t, id, ext.sessionID)
	assert.False(t, ext.newSession)
}
