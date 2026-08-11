package session

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Entry type tags that [MarshalEntry] writes to the wire.
const (
	typeMessage    = "message"
	typeCompaction = "compaction"
	typeCustom     = "custom"
)

// customRegistry maps a custom entry Kind to its concrete Go type so
// [UnmarshalEntry] can reconstruct it.
var customRegistry = map[string]reflect.Type{}

// RegisterCustom registers an application-defined custom entry type so a
// [Store] can decode it from a persisted log. The prototype is a zero
// value of the type that embeds [CustomEntry], for example
// ArtifactEntry{}. The kind must match its [CustomEntry.Kind]. A second
// registration of the same kind panics.
//
// An unregistered kind decodes to a bare [CustomEntry] that holds the
// header and the kind. Registration is therefore necessary only to
// recover the application-defined fields.
func RegisterCustom(kind string, prototype Entry) {
	if kind == "" {
		panic("session: RegisterCustom requires a non-empty kind")
	}
	if _, dup := customRegistry[kind]; dup {
		panic(fmt.Sprintf("session: custom kind %q already registered", kind))
	}
	customRegistry[kind] = reflect.TypeOf(prototype)
}

type messageWire struct {
	Type string `json:"type"`
	EntryHeader
	Meta    bool       `json:"meta,omitempty"`
	Message ai.Message `json:"message"`
}

type compactionWire struct {
	Type string `json:"type"`
	EntryHeader
	Summary      string `json:"summary"`
	FirstKeptID  string `json:"first_kept_id"`
	TokensBefore int    `json:"tokens_before"`
}

// MarshalEntry encodes an [Entry] to a single JSON object. The object
// carries a type tag.
func MarshalEntry(e Entry) ([]byte, error) {
	switch v := e.(type) {
	case MessageEntry:
		return json.Marshal(messageWire{
			Type:        typeMessage,
			EntryHeader: v.EntryHeader,
			Meta:        v.Meta,
			Message:     v.Message,
		})
	case CompactionEntry:
		return json.Marshal(compactionWire{
			Type:         typeCompaction,
			EntryHeader:  v.EntryHeader,
			Summary:      v.Summary,
			FirstKeptID:  v.FirstKeptID,
			TokensBefore: v.TokensBefore,
		})
	default:
		return marshalCustom(e)
	}
}

// marshalCustom encodes an application-defined entry. It marshals the
// full struct — header, kind, and application fields — and then tags the
// object as custom.
func marshalCustom(e Entry) ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["type"], _ = json.Marshal(typeCustom)
	return json.Marshal(fields)
}

// UnmarshalEntry decodes a single JSON object that [MarshalEntry]
// produced. It reconstructs a custom entry from the type registered with
// [RegisterCustom]. An unregistered kind decodes to a bare
// [CustomEntry].
func UnmarshalEntry(data []byte) (Entry, error) {
	var probe struct {
		Type string `json:"type"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	switch probe.Type {
	case typeMessage:
		var w messageWire
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return MessageEntry{EntryHeader: w.EntryHeader, Message: w.Message, Meta: w.Meta}, nil
	case typeCompaction:
		var w compactionWire
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return CompactionEntry{
			EntryHeader:  w.EntryHeader,
			Summary:      w.Summary,
			FirstKeptID:  w.FirstKeptID,
			TokensBefore: w.TokensBefore,
		}, nil
	case typeCustom:
		return unmarshalCustom(data, probe.Kind)
	default:
		return nil, fmt.Errorf("session: unknown entry type %q", probe.Type)
	}
}

func unmarshalCustom(data []byte, kind string) (Entry, error) {
	rt, ok := customRegistry[kind]
	if !ok {
		var w struct {
			EntryHeader
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return CustomEntry{EntryHeader: w.EntryHeader, Kind: w.Kind}, nil
	}

	ptr := reflect.New(rt)
	if err := json.Unmarshal(data, ptr.Interface()); err != nil {
		return nil, err
	}
	e, ok := ptr.Elem().Interface().(Entry)
	if !ok {
		return nil, fmt.Errorf("session: registered type for kind %q does not implement Entry", kind)
	}
	return e, nil
}
