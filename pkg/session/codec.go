package session

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Entry type tags written to the wire by [MarshalEntry].
const (
	typeMessage    = "message"
	typeCompaction = "compaction"
	typeState      = "state"
	typeCustom     = "custom"
)

// customRegistry maps a custom entry Kind to its concrete Go type so
// [UnmarshalEntry] can reconstruct it.
var customRegistry = map[string]reflect.Type{}

// RegisterCustom registers an application-defined custom entry type so a
// [Store] can decode it from a persisted log. prototype is a zero value
// of the type embedding [CustomEntry] (e.g. ArtifactEntry{}); kind must
// match its [CustomEntry.Kind]. Registering the same kind twice panics.
//
// Unregistered kinds decode to a bare [CustomEntry] (header + kind), so
// registration is only needed to recover the app-defined fields.
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
	FirstKeptID  string `json:"firstKeptId"`
	TokensBefore int    `json:"tokensBefore"`
}

type stateWire[T any] struct {
	Type string `json:"type"`
	EntryHeader
	State T `json:"state"`
}

// MarshalEntry encodes an [Entry] to a single JSON object tagged by type.
// T is the session state type, needed to encode a [StateEntry].
func MarshalEntry[T any](e Entry) ([]byte, error) {
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
	case StateEntry[T]:
		return json.Marshal(stateWire[T]{
			Type:        typeState,
			EntryHeader: v.EntryHeader,
			State:       v.State,
		})
	default:
		return marshalCustom(e)
	}
}

// marshalCustom encodes an application-defined entry by marshaling its
// full struct (header + kind + app fields) and tagging it as custom.
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

// UnmarshalEntry decodes a single JSON object produced by [MarshalEntry].
// T is the session state type, needed to decode a [StateEntry]. Custom
// entries are reconstructed via the type registered with [RegisterCustom];
// unregistered kinds decode to a bare [CustomEntry].
func UnmarshalEntry[T any](data []byte) (Entry, error) {
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
	case typeState:
		var w stateWire[T]
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return StateEntry[T]{EntryHeader: w.EntryHeader, State: w.State}, nil
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
