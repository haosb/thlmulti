package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"go.rockorager.dev/vaxis/widgets/term"
)

// historyLen reads back the number of lines the emulator is holding, the same
// way setScrollback writes the limit.
func historyLen(t *testing.T, vt *term.Model) int {
	t.Helper()
	state := reflect.ValueOf(vt).Elem().FieldByName("primaryScreen").FieldByName("state")
	state = reflect.NewAt(state.Type(), unsafe.Pointer(state.UnsafeAddr())).Elem()
	n := state.Elem().FieldByName("scrollback").FieldByName("len")
	return int(reflect.NewAt(n.Type(), unsafe.Pointer(n.UnsafeAddr())).Elem().Int())
}

func flood(vt *term.Model, lines int) {
	for i := range lines {
		vt.WriteString(fmt.Sprintf("line %d\r\n", i))
	}
}

// The point of the whole file: a configured limit has to actually bound the
// history, because the history is what costs the memory.
func TestSetScrollbackBoundsHistory(t *testing.T) {
	for _, limit := range []int{0, 1, 25} {
		vt := term.New()
		vt.Resize(20, 5)
		if err := setScrollback(vt, limit); err != nil {
			t.Fatalf("limit %d: %v", limit, err)
		}
		flood(vt, 200)
		if got := historyLen(t, vt); got != limit {
			t.Errorf("limit %d: kept %d lines", limit, got)
		}
	}
}

// A limit only bounds history: it must not cost you the lines on screen.
func TestSetScrollbackKeepsVisibleRows(t *testing.T) {
	vt := term.New()
	vt.Resize(20, 5)
	if err := setScrollback(vt, 0); err != nil {
		t.Fatal(err)
	}
	flood(vt, 200)
	if got, want := strings.TrimRight(vt.RowString(3), " "), "line 199"; got != want {
		t.Errorf("last visible row = %q, want %q", got, want)
	}
}

// setScrollback reaches into vaxis. If a vaxis upgrade renames or moves what it
// reaches for, that has to surface as an error rather than as a silent no-op.
func TestSetScrollbackReportsAMissingBuffer(t *testing.T) {
	if err := setScrollback(term.New(), 10); err == nil {
		t.Error("a model that was never sized should report an error")
	}
}
