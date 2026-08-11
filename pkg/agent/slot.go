package agent

// Slot is a typed view of one extension key. A sub-package declares one
// Slot for its key. It uses the Slot to create options and to read its
// configuration back from a [Config]. The Slot replaces manual type
// assertions on [Config.Extensions].
type Slot[T any] struct{ Key string }

// Mutate returns an [Option] that reads the current value of the slot,
// passes it to fn, and stores the result. If the slot is absent or holds
// another type, the value is the zero T. Mutate wraps
// [WithExtensionMutator].
func (s Slot[T]) Mutate(fn func(T) T) Option {
	return WithExtensionMutator(s.Key, func(v any) any {
		cur, _ := v.(T)
		return fn(cur)
	})
}

// From reads the value of the slot from cfg. If the slot is absent or
// holds another type, From returns the zero T.
func (s Slot[T]) From(cfg Config) T {
	v, _ := cfg.Extensions[s.Key].(T)
	return v
}
