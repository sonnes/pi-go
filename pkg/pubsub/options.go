package pubsub

// Default configuration values.
const (
	defaultBufferSize = 64
	defaultMaxEvents  = 1000
)

// Option configures a Broker.
type Option func(*brokerOptions)

// brokerOptions holds all possible options for broker configuration.
type brokerOptions struct {
	bufferSize int
	maxEvents  int
	blocking   bool
}

// WithBufferSize sets the channel buffer size for subscriber channels.
// A larger buffer allows subscribers to temporarily fall behind without
// losing events. Default is 64.
func WithBufferSize(size int) Option {
	return func(o *brokerOptions) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// WithMaxEvents sets the maximum number of events to retain.
// This can be used for event history/replay functionality.
// Default is 1000.
func WithMaxEvents(max int) Option {
	return func(o *brokerOptions) {
		if max > 0 {
			o.maxEvents = max
		}
	}
}

// WithBlockingPublish configures the broker to block on publish when a
// subscriber's channel is full, instead of dropping the event. The publish
// unblocks if the broker is shut down.
func WithBlockingPublish() Option {
	return func(o *brokerOptions) {
		o.blocking = true
	}
}

// applyOptions applies the given options to a new brokerOptions struct.
func applyOptions(opts []Option) *brokerOptions {
	o := &brokerOptions{
		bufferSize: defaultBufferSize,
		maxEvents:  defaultMaxEvents,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
