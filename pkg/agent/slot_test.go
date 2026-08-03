package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
)

type slotExt struct {
	name  string
	count int
}

func TestSlotMutate(t *testing.T) {
	slot := agent.Slot[slotExt]{Key: "ext"}

	tests := []struct {
		name string
		opts []agent.Option
		want slotExt
	}{
		{
			name: "empty config yields fn(zero)",
			opts: []agent.Option{
				slot.Mutate(func(e slotExt) slotExt {
					e.name = "first"
					return e
				}),
			},
			want: slotExt{
				name: "first",
			},
		},
		{
			name: "repeated mutates layer",
			opts: []agent.Option{
				slot.Mutate(func(e slotExt) slotExt {
					e.name = "first"
					e.count++
					return e
				}),
				slot.Mutate(func(e slotExt) slotExt {
					require.Equal(t, "first", e.name)
					e.count++
					return e
				}),
			},
			want: slotExt{
				name:  "first",
				count: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := agent.ApplyOptions(tt.opts...)
			assert.Equal(t, tt.want, slot.From(cfg))
		})
	}
}

func TestSlotFrom(t *testing.T) {
	slot := agent.Slot[slotExt]{Key: "ext"}

	t.Run("absent slot returns zero", func(t *testing.T) {
		cfg := agent.ApplyOptions()
		assert.Equal(t, slotExt{}, slot.From(cfg))
	})

	t.Run("returns stored value after ApplyOptions", func(t *testing.T) {
		cfg := agent.ApplyOptions(
			slot.Mutate(func(e slotExt) slotExt {
				e.name = "stored"
				return e
			}),
		)
		assert.Equal(t, slotExt{name: "stored"}, slot.From(cfg))
	})

	t.Run("wrong type under key returns zero, no panic", func(t *testing.T) {
		ptrSlot := agent.Slot[*slotExt]{Key: "ext"}
		cfg := agent.ApplyOptions(
			agent.WithExtension("ext", "not a *slotExt"),
		)
		require.NotPanics(t, func() {
			assert.Nil(t, ptrSlot.From(cfg))
		})
	})
}

func TestSlotIsolation(t *testing.T) {
	a := agent.Slot[slotExt]{Key: "a"}
	b := agent.Slot[slotExt]{Key: "b"}

	cfg := agent.ApplyOptions(
		a.Mutate(func(e slotExt) slotExt {
			e.name = "alpha"
			return e
		}),
		b.Mutate(func(e slotExt) slotExt {
			e.name = "beta"
			return e
		}),
	)

	assert.Equal(t, slotExt{name: "alpha"}, a.From(cfg))
	assert.Equal(t, slotExt{name: "beta"}, b.From(cfg))
}

func TestSlotComposesWithCoreOptions(t *testing.T) {
	slot := agent.Slot[slotExt]{Key: "ext"}

	cfg := agent.ApplyOptions(
		agent.WithMaxTurns(7),
		slot.Mutate(func(e slotExt) slotExt {
			e.name = "flat"
			return e
		}),
	)

	assert.Equal(t, 7, cfg.MaxTurns)
	assert.Equal(t, slotExt{name: "flat"}, slot.From(cfg))
}
