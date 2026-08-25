package main

import (
	"fmt"
	"reflect"
	"unsafe"

	"go.rockorager.dev/vaxis/widgets/term"
)

// bytesPerCell is what one cell of history costs: widgets/term stores a
// vaxis.Cell plus two flags, and history is kept as whole rows of full width
// with no trimming of trailing blanks. So a tab's history costs roughly
//
//	scrollback * columns * bytesPerCell
//
// which at the library's own default of 10000 lines and a wide window is a few
// hundred megabytes per tab. That is the entire reason this knob exists.
const bytesPerCell = 88

// setScrollback caps how many lines of history a tab keeps.
//
// widgets/term hardcodes 10000 and exposes no way to change it, so we reach in
// and set the field. That is a liberty, and it is taken deliberately: the
// alternative is carrying a 9000-line fork of the emulator for one integer.
// Every step is checked by name and kind, so a vaxis upgrade that moves this
// field fails here with a clear message instead of silently doing nothing —
// see run, which reports the failure and carries on with the library default.
//
// The limit lives behind a pointer shared by every view of the primary screen,
// so setting it once covers the active screen too. It has to be re-applied
// after a resize, and after anything else that rebuilds the buffer.
func setScrollback(vt *term.Model, lines int) error {
	screen := reflect.ValueOf(vt).Elem().FieldByName("primaryScreen")
	if !screen.IsValid() || screen.Kind() != reflect.Struct {
		return fmt.Errorf("term.Model has no primaryScreen struct field")
	}
	state := screen.FieldByName("state")
	if !state.IsValid() || state.Kind() != reflect.Pointer {
		return fmt.Errorf("term.Model.primaryScreen has no state pointer field")
	}
	// The field is unexported, so read it through its address rather than with
	// Interface, which would panic.
	state = reflect.NewAt(state.Type(), unsafe.Pointer(state.UnsafeAddr())).Elem()
	if state.IsNil() {
		return fmt.Errorf("the screen buffer has not been created yet")
	}
	limit := state.Elem().FieldByName("scrollbackLimit")
	if !limit.IsValid() || limit.Kind() != reflect.Int {
		return fmt.Errorf("the screen buffer has no scrollbackLimit int field")
	}
	reflect.NewAt(limit.Type(), unsafe.Pointer(limit.UnsafeAddr())).Elem().SetInt(int64(lines))
	return nil
}
