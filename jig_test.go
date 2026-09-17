//go:build jig

// Hardware-gated tests against a physical go-usb-jig FX2 board.
//
// Excluded from normal `go test ./...` by the jig build tag, since these
// need real hardware attached: run with `go test -tags jig ./...`. Each test
// skips cleanly, rather than failing, if the board isn't found -- a run
// without hardware attached should say so, not look like a broken test.
//
// Endpoint map matches firmware/hw/go-usb-jig/dscr.a51 exactly; keep them in
// sync by hand, nothing generates one from the other. Isochronous and vendor
// stall/RAM commands were in the original (broken) firmware design and
// aren't back yet in the fx2lib-based rebuild -- see the top-level README's
// "Firmware status" section for the two known real bugs in what's here now.

package jig

import (
	"bytes"
	"testing"
	"time"

	usb "github.com/kevmo314/go-usb"
)

const (
	jigVendorID  = 0x0925
	jigProductID = 0x3881
)

const (
	epInterruptIn = 0x81
	epBulkOut     = 0x02
	epBulkIn      = 0x86
)

// openJig finds and opens the board and claims its one interface, which has
// only one alternate setting -- it exposes every test endpoint from the
// start, unlike the multi-alt-setting stock firmware this board originally
// shipped with. It skips the test if the board isn't attached.
//
// SetConfiguration is called unconditionally, even when GetConfiguration
// already reports 1: IOKit does not reliably create the interface's child
// services from macOS's own automatic configuration alone, found the hard
// way when ClaimInterface kept failing with ErrDeviceNotFound despite the
// device already reporting itself configured.
func openJig(t *testing.T) *usb.DeviceHandle {
	t.Helper()

	devices, err := usb.DeviceList()
	if err != nil {
		t.Fatalf("DeviceList: %v", err)
	}

	var jig *usb.Device
	for _, d := range devices {
		if d.Descriptor.VendorID == jigVendorID && d.Descriptor.ProductID == jigProductID {
			jig = d
			break
		}
	}
	if jig == nil {
		t.Skip("go-usb-jig board not attached")
	}

	handle, err := jig.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { handle.Close() })

	if err := handle.SetConfiguration(1); err != nil {
		t.Fatalf("SetConfiguration(1): %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	if err := handle.ClaimInterface(0); err != nil {
		t.Fatalf("ClaimInterface(0): %v", err)
	}
	t.Cleanup(func() { handle.ReleaseInterface(0) })

	return handle
}

// TestBulkLoopback writes a known pattern to EP2 OUT and reads it back from
// EP6 IN, which the firmware is meant to echo it to. Known broken right now:
// EP6 IN returns a fixed repeating pattern instead of the echoed data --
// the mechanism works end to end (this is real data, not an error), but the
// firmware's copy loop has a bug. See README.
func TestBulkLoopback(t *testing.T) {
	handle := openJig(t)

	want := make([]byte, 64)
	for i := range want {
		want[i] = byte(i)
	}

	if _, err := handle.BulkTransfer(epBulkOut, want, 2*time.Second); err != nil {
		t.Fatalf("BulkTransfer OUT: %v", err)
	}

	got := make([]byte, 64)
	n, err := handle.BulkTransfer(epBulkIn, got, 2*time.Second)
	if err != nil {
		t.Fatalf("BulkTransfer IN: %v", err)
	}
	got = got[:n]

	if !bytes.Equal(got, want) {
		t.Errorf("echoed data does not match: got % x, want % x", got, want)
	}
}

// TestInterruptHeartbeat reads EP1 IN twice and checks the byte advanced.
// Known broken right now: returns kIOReturnBadArgument. See README.
func TestInterruptHeartbeat(t *testing.T) {
	handle := openJig(t)

	buf := make([]byte, 1)
	n, err := handle.InterruptTransfer(epInterruptIn, buf, 2*time.Second)
	if err != nil {
		t.Fatalf("InterruptTransfer 1: %v", err)
	}
	first := buf[:n]

	time.Sleep(10 * time.Millisecond)

	n, err = handle.InterruptTransfer(epInterruptIn, buf, 2*time.Second)
	if err != nil {
		t.Fatalf("InterruptTransfer 2: %v", err)
	}
	second := buf[:n]

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1-byte heartbeat, got %d then %d bytes", len(first), len(second))
	}
	if first[0] == second[0] {
		t.Errorf("heartbeat did not advance: both reads returned %d", first[0])
	}
}
