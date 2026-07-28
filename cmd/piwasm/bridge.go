//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"syscall/js"

	"github.com/sonnes/pi-go/cmd/piwasm/internal/demo"
)

// bridge exposes the demo to the page as globalThis.piDemo:
//
//	piDemo.start(onEvent)   // onEvent(jsonString) for every event
//	piDemo.send(jsonString) // one demo.Command
//
// Commands run on a goroutine because syscall/js callbacks execute on
// the browser's only thread — handling a run inline would freeze the
// page for the length of the turn.
type bridge struct {
	demo *demo.Demo
	emit js.Value
}

func (b *bridge) install() {
	js.Global().Set("piDemo", js.ValueOf(map[string]any{
		"start": js.FuncOf(b.start),
		"send":  js.FuncOf(b.send),
	}))
}

// start wires the page's callback and opens a scripted session.
func (b *bridge) start(_ js.Value, args []js.Value) any {
	if len(args) == 0 || args[0].Type() != js.TypeFunction {
		return errorEvent("piDemo.start needs an event callback")
	}

	b.emit = args[0]

	d, err := demo.New(context.Background(), b.publish)
	if err != nil {
		return errorEvent(err.Error())
	}

	b.demo = d

	// Send the page an empty tree so it can render before any input.
	go b.handle(demo.Command{Kind: demo.CmdReset})

	return nil
}

// send queues one command.
func (b *bridge) send(_ js.Value, args []js.Value) any {
	if b.demo == nil {
		return errorEvent("piDemo.send before piDemo.start")
	}
	if len(args) == 0 {
		return errorEvent("piDemo.send needs a command")
	}

	var cmd demo.Command
	if err := json.Unmarshal([]byte(args[0].String()), &cmd); err != nil {
		return errorEvent("bad command: " + err.Error())
	}

	go b.handle(cmd)

	return nil
}

func (b *bridge) handle(cmd demo.Command) {
	// Handle already emits an error event before returning one.
	_ = b.demo.Handle(context.Background(), cmd)
}

// publish hands one event to the page as JSON.
func (b *bridge) publish(e demo.Event) {
	if b.emit.IsUndefined() {
		return
	}

	data, err := json.Marshal(e)
	if err != nil {
		data, _ = json.Marshal(demo.Event{Kind: demo.KindError, Text: err.Error()})
	}

	b.emit.Invoke(string(data))
}

// errorEvent is returned synchronously for misuse of the API itself,
// where there is no session to publish through yet.
func errorEvent(msg string) any {
	data, _ := json.Marshal(demo.Event{Kind: demo.KindError, Text: msg})

	return string(data)
}
