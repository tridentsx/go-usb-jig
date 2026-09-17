//go:build jig && !darwin

package jig

import "testing"

// TestIsochronousCounter is a placeholder on every platform but macOS (see
// isochronous_darwin_test.go for the real test).
//
// go-usb has no working isochronous entry point on this platform yet.
// Linux's real one is DeviceHandle.NewIsochronousTransfer +
// (*IsochronousTransfer).Submit/Wait (see go-usb's isochronous_linux.go) --
// untested against this board, not yet wired up here. Windows' equivalent
// is currently a hard stub (isochronous_windows.go) pending real
// implementation.
//
// This is deliberately a skip, not an absent test: a run on this platform
// should say isochronous coverage is missing, not silently omit it.
func TestIsochronousCounter(t *testing.T) {
	t.Skip("isochronous transfers not implemented against this board on this platform yet")
}
