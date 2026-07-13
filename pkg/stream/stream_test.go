package stream_test

import (
	"errors"
	"testing"

	"github.com/sonnes/pi-go/pkg/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream_EventsThenWait(t *testing.T) {
	s := stream.New(func(push func(string)) (int, error) {
		push("start")
		push("end")
		return 42, nil
	})

	var events []string
	for e, err := range s.Events() {
		require.NoError(t, err)
		events = append(events, e)
	}

	assert.Equal(t, []string{"start", "end"}, events)

	result, err := s.Wait()
	require.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestStream_WaitWithoutEvents(t *testing.T) {
	s := stream.New(func(push func(string)) (int, error) {
		push("start")
		push("end")
		return 7, nil
	})

	result, err := s.Wait()
	require.NoError(t, err)
	assert.Equal(t, 7, result)
}

func TestStream_ErrorEndsIteration(t *testing.T) {
	wantErr := errors.New("boom")
	s := stream.New(func(push func(string)) (int, error) {
		push("start")
		return 0, wantErr
	})

	var events []string
	var gotErr error
	for e, err := range s.Events() {
		if err != nil {
			gotErr = err
			break
		}
		events = append(events, e)
	}

	assert.Equal(t, []string{"start"}, events)
	assert.ErrorIs(t, gotErr, wantErr)
}

func TestStream_WaitReturnsErrorAndPartialResult(t *testing.T) {
	wantErr := errors.New("boom")
	s := stream.New(func(push func(string)) (int, error) {
		return 3, wantErr
	})

	result, err := s.Wait()
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, 3, result)
}

// Breaking out of Events must not deadlock the producer; a later Wait
// still returns the final result.
func TestStream_BreakEarlyThenWait(t *testing.T) {
	s := stream.New(func(push func(int)) (string, error) {
		// Push more events than the channel buffers to prove pushes
		// after the consumer breaks do not block forever.
		for i := range 64 {
			push(i)
		}
		return "done", nil
	})

	for range s.Events() {
		break
	}

	result, err := s.Wait()
	require.NoError(t, err)
	assert.Equal(t, "done", result)
}

func TestStream_WaitAfterFullIteration(t *testing.T) {
	s := stream.New(func(push func(string)) (string, error) {
		push("only")
		return "done", nil
	})

	for _, err := range s.Events() {
		require.NoError(t, err)
	}

	result, err := s.Wait()
	require.NoError(t, err)
	assert.Equal(t, "done", result)
}

func TestErr_FailsImmediately(t *testing.T) {
	wantErr := errors.New("bad input")
	s := stream.Err[string, int](wantErr)

	var sawEvent bool
	var gotErr error
	for _, err := range s.Events() {
		if err != nil {
			gotErr = err
			break
		}
		sawEvent = true
	}

	assert.False(t, sawEvent)
	assert.ErrorIs(t, gotErr, wantErr)

	_, err := s.Wait()
	assert.ErrorIs(t, err, wantErr)
}
