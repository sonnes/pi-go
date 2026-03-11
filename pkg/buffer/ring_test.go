package buffer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRing(t *testing.T) {
	r := NewRing[string](10)

	assert.Equal(t, 0, r.Len())
	assert.Equal(t, 10, r.Cap())
}

func TestNewRing_MinCapacity(t *testing.T) {
	r := NewRing[string](0)
	assert.Equal(t, 1, r.Cap())

	r = NewRing[string](-5)
	assert.Equal(t, 1, r.Cap())
}

func TestRing_Write(t *testing.T) {
	r := NewRing[string](3)

	seq := r.Write("a")
	assert.Equal(t, int64(1), seq)
	assert.Equal(t, 1, r.Len())

	seq = r.Write("b")
	assert.Equal(t, int64(2), seq)
	assert.Equal(t, 2, r.Len())
}

func TestRing_Write_Eviction(t *testing.T) {
	r := NewRing[string](3)

	r.Write("a")
	r.Write("b")
	r.Write("c")
	assert.Equal(t, 3, r.Len())

	// 4th write evicts oldest
	r.Write("d")
	assert.Equal(t, 3, r.Len())

	entries := r.After(0)
	require.Len(t, entries, 3)
	assert.Equal(t, "b", entries[0].Value)
	assert.Equal(t, "c", entries[1].Value)
	assert.Equal(t, "d", entries[2].Value)
}

func TestRing_After(t *testing.T) {
	r := NewRing[string](10)

	r.Write("a") // seq 1
	r.Write("b") // seq 2
	r.Write("c") // seq 3

	tests := []struct {
		name     string
		after    int64
		wantVals []string
		wantSeqs []int64
	}{
		{
			name:     "after 0 returns all",
			after:    0,
			wantVals: []string{"a", "b", "c"},
			wantSeqs: []int64{1, 2, 3},
		},
		{
			name:     "after 1 returns b and c",
			after:    1,
			wantVals: []string{"b", "c"},
			wantSeqs: []int64{2, 3},
		},
		{
			name:     "after 3 returns nothing",
			after:    3,
			wantVals: nil,
			wantSeqs: nil,
		},
		{
			name:     "after 100 returns nothing",
			after:    100,
			wantVals: nil,
			wantSeqs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := r.After(tt.after)
			var vals []string
			var seqs []int64
			for _, e := range entries {
				vals = append(vals, e.Value)
				seqs = append(seqs, e.Seq)
			}
			assert.Equal(t, tt.wantVals, vals)
			assert.Equal(t, tt.wantSeqs, seqs)
		})
	}
}

func TestRing_After_WithEviction(t *testing.T) {
	r := NewRing[int](3)

	r.Write(1) // seq 1, evicted
	r.Write(2) // seq 2, evicted
	r.Write(3) // seq 3
	r.Write(4) // seq 4
	r.Write(5) // seq 5

	// seq 1 and 2 are evicted, after(0) returns only what's buffered
	entries := r.After(0)
	require.Len(t, entries, 3)
	assert.Equal(t, int64(3), entries[0].Seq)
	assert.Equal(t, 3, entries[0].Value)

	// after(2) should return seq 3, 4, 5
	entries = r.After(2)
	require.Len(t, entries, 3)
	assert.Equal(t, int64(3), entries[0].Seq)

	// after(3) should return seq 4, 5
	entries = r.After(3)
	require.Len(t, entries, 2)
	assert.Equal(t, int64(4), entries[0].Seq)
	assert.Equal(t, int64(5), entries[1].Seq)
}

func TestRing_WrapAround(t *testing.T) {
	r := NewRing[int](3)

	// Fill: [0, 1, 2]
	for i := range 3 {
		r.Write(i)
	}

	// Overwrite: [3, 4, 5]
	for i := 3; i < 6; i++ {
		r.Write(i)
	}

	entries := r.After(0)
	require.Len(t, entries, 3)
	assert.Equal(t, 3, entries[0].Value)
	assert.Equal(t, 4, entries[1].Value)
	assert.Equal(t, 5, entries[2].Value)

	// Sequences are monotonically increasing
	assert.Equal(t, int64(4), entries[0].Seq)
	assert.Equal(t, int64(5), entries[1].Seq)
	assert.Equal(t, int64(6), entries[2].Seq)
}

func TestRing_Oldest(t *testing.T) {
	r := NewRing[string](3)

	_, ok := r.Oldest()
	assert.False(t, ok, "empty ring has no oldest")

	r.Write("a")
	e, ok := r.Oldest()
	require.True(t, ok)
	assert.Equal(t, "a", e.Value)
	assert.Equal(t, int64(1), e.Seq)

	r.Write("b")
	r.Write("c")
	r.Write("d") // evicts "a"

	e, ok = r.Oldest()
	require.True(t, ok)
	assert.Equal(t, "b", e.Value)
	assert.Equal(t, int64(2), e.Seq)
}

func TestRing_Newest(t *testing.T) {
	r := NewRing[string](3)

	_, ok := r.Newest()
	assert.False(t, ok, "empty ring has no newest")

	r.Write("a")
	e, ok := r.Newest()
	require.True(t, ok)
	assert.Equal(t, "a", e.Value)
	assert.Equal(t, int64(1), e.Seq)

	r.Write("b")
	r.Write("c")

	e, ok = r.Newest()
	require.True(t, ok)
	assert.Equal(t, "c", e.Value)
	assert.Equal(t, int64(3), e.Seq)
}
