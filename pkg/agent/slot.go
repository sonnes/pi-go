package agent

// Slot is a typed view over one extension key. A sub-package declares
// one Slot for its key and uses it to mint options and read its
// configuration back out of a [Config], replacing manual type
// assertions on [Config.Extensions].
type Slot[T any] struct{ Key string }

// Mutate returns an [Option] that reads the slot's current value
// (the zero T if absent or of another type), passes it to fn, and
// stores the result. Wraps [WithExtensionMutator].
func (s Slot[T]) Mutate(fn func(T) T) Option {
	return WithExtensionMutator(s.Key, func(v any) any {
		cur, _ := v.(T)
		return fn(cur)
	})
}

// From reads the slot's value out of cfg, returning the zero T if the
// slot is absent or holds another type.
func (s Slot[T]) From(cfg Config) T {
	v, _ := cfg.Extensions[s.Key].(T)
	return v
}
