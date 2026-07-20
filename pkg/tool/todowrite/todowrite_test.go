package todowrite

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runWith(t *testing.T, in Input) (string, error) {
	t.Helper()
	run := New()
	return run(context.Background(), in)
}

func item(content, active string, status Status) Item {
	return Item{Content: content, ActiveForm: active, Status: status}
}

func TestTodoWrite_ValidListReturnsAck(t *testing.T) {
	body, err := runWith(t, Input{Todos: []Item{
		item("Write tests", "Writing tests", StatusInProgress),
		item("Ship it", "Shipping it", StatusPending),
	}})
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(body), "successfully")
}

func TestTodoWrite_SingleItem(t *testing.T) {
	body, err := runWith(t, Input{Todos: []Item{
		item("Do the thing", "Doing the thing", StatusInProgress),
	}})
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(body), "successfully")
}

func TestTodoWrite_EmptyListAllowed(t *testing.T) {
	body, err := runWith(t, Input{Todos: []Item{}})
	require.NoError(t, err, "clearing the list is allowed")
	assert.Contains(t, strings.ToLower(body), "successfully")
}

func TestTodoWrite_AllCompletedAllowed(t *testing.T) {
	body, err := runWith(t, Input{Todos: []Item{
		item("First", "Doing first", StatusCompleted),
		item("Second", "Doing second", StatusCompleted),
	}})
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(body), "successfully")
}

func TestTodoWrite_RejectsMissingContent(t *testing.T) {
	_, err := runWith(t, Input{Todos: []Item{
		item("", "Doing it", StatusPending),
	}})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "content")
}

func TestTodoWrite_RejectsMissingActiveForm(t *testing.T) {
	_, err := runWith(t, Input{Todos: []Item{
		item("Do it", "", StatusPending),
	}})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "active_form")
}

func TestTodoWrite_RejectsInvalidStatus(t *testing.T) {
	_, err := runWith(t, Input{Todos: []Item{
		item("Do it", "Doing it", Status("blocked")),
	}})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "status")
}

func TestTodoWrite_RejectsMultipleInProgress(t *testing.T) {
	_, err := runWith(t, Input{Todos: []Item{
		item("First", "Doing first", StatusInProgress),
		item("Second", "Doing second", StatusInProgress),
	}})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "in_progress")
}

func TestTodoWrite_ErrorIncludesShapeExample(t *testing.T) {
	_, err := runWith(t, Input{Todos: []Item{
		item("", "Doing it", StatusPending),
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Expected shape:", "error should help the model self-correct")
}
