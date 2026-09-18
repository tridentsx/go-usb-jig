//go:build jig

// Hardware-gated tests against a physical go-usb-jig FX2 board.
//
// Excluded from normal `go test ./...` by the jig build tag, since these
// need real hardware attached: run with `go test -tags jig ./...`. Each test
// skips cleanly, rather than failing, if the board isn't found -- a run
// without hardware attached should say so, not look like a broken test.
//
// Endpoint map matches firmware/hw/go-usb-jig/dscr.a51 exactly; keep them in
// sync by hand, nothing generates one from the other. See the top-level
// README's "Firmware status" section for the current state of each piece.

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

// Vendor request numbers for the scratch-RAM read/write test; see
// VR_READ_RAM/VR_WRITE_RAM in firmware/hw/go-usb-jig/main.c.
const (
	vrReadRAM  = 0xB6
	vrWriteRAM = 0xB7
)

// reqTypeEndpointOut is the standard bmRequestType for a host-to-device
// request targeting an endpoint (used with SET_FEATURE/CLEAR_FEATURE below).
const reqTypeEndpointOut = 0x02

// featureEndpointHalt is the standard USB feature selector for stalling/
// clearing an endpoint's halt condition.
const featureEndpointHalt = 0

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
//
// SetInterfaceAltSetting(0, 0) after claiming forces a SET_INTERFACE
// request, which the firmware's handle_set_interface (see
// firmware/hw/go-usb-jig/main.c) uses to reset EP2/EP6's FIFOs and data
// toggles. Without it, packets left over in EP6 IN's quad buffer from a
// previous test run in the same process (no reflash between runs) can
// surface on the next BulkTransfer read, making TestBulkLoopback flaky.
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

	if err := handle.SetInterfaceAltSetting(0, 0); err != nil {
		t.Fatalf("SetInterfaceAltSetting(0, 0): %v", err)
	}

	return handle
}

// TestBulkLoopback writes a known pattern to EP2 OUT and reads it back from
// EP6 IN, which the firmware echoes it to.
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

// readInterrupt reads one byte from EP1 IN, retrying a bounded number of
// times on kIOReturnAborted. Claiming and releasing the interface's pipes
// repeatedly across back-to-back test runs (no device reset in between)
// occasionally leaves a stale in-flight request that IOKit reports as an
// abort on the very next read; a short retry clears it without masking any
// other error.
func readInterrupt(t *testing.T, handle *usb.DeviceHandle, buf []byte) int {
	t.Helper()
	const maxAttempts = 3
	for attempt := 1; ; attempt++ {
		n, err := handle.InterruptTransfer(epInterruptIn, buf, 2*time.Second)
		if err == nil {
			return n
		}
		if attempt == maxAttempts {
			t.Fatalf("InterruptTransfer: %v (after %d attempts)", err, attempt)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestInterruptHeartbeat reads EP1 IN twice into separate buffers and checks
// the byte advanced between reads.
func TestInterruptHeartbeat(t *testing.T) {
	handle := openJig(t)

	buf1 := make([]byte, 1)
	n := readInterrupt(t, handle, buf1)
	first := buf1[:n]

	// EP1 IN has bInterval=8 in the high-speed descriptor, which the host
	// controller decodes as a 2^(8-1) = 128 microframe (16ms) polling
	// interval; a shorter gap can read the same not-yet-repolled packet
	// twice.
	time.Sleep(25 * time.Millisecond)

	buf2 := make([]byte, 1)
	n = readInterrupt(t, handle, buf2)
	second := buf2[:n]

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1-byte heartbeat, got %d then %d bytes", len(first), len(second))
	}
	if first[0] == second[0] {
		t.Errorf("heartbeat did not advance: both reads returned %d", first[0])
	}
}

// TestVendorRAM writes a pattern spanning more than one 64-byte EP0 packet
// to the firmware's scratch RAM via a custom vendor request, then reads it
// back via a second custom vendor request, exercising a real multi-packet
// EP0 control-transfer data stage in both directions.
func TestVendorRAM(t *testing.T) {
	handle := openJig(t)

	want := []byte("go-usb-jig vendor RAM roundtrip test, spanning more than one 64-byte EP0 packet!!")
	if _, err := handle.ControlTransfer(0x40, vrWriteRAM, 0, 0, want, 2*time.Second); err != nil {
		t.Fatalf("vendor WRITE_RAM: %v", err)
	}

	got := make([]byte, len(want))
	n, err := handle.ControlTransfer(0xC0, vrReadRAM, 0, 0, got, 2*time.Second)
	if err != nil {
		t.Fatalf("vendor READ_RAM: %v", err)
	}
	got = got[:n]

	if !bytes.Equal(got, want) {
		t.Errorf("vendor RAM roundtrip mismatch: got % x, want % x", got, want)
	}
}

// TestEndpointStall exercises the standard SET_FEATURE/CLEAR_FEATURE
// (ENDPOINT_HALT) requests against a real stalled pipe: fx2lib's setupdat.c
// implements the actual stall/unstall (see handle_set_feature/
// handle_clear_feature there), no custom firmware needed.
func TestEndpointStall(t *testing.T) {
	handle := openJig(t)

	if err := handle.SetFeature(reqTypeEndpointOut, featureEndpointHalt, epBulkIn); err != nil {
		t.Fatalf("SetFeature(ENDPOINT_HALT): %v", err)
	}

	buf := make([]byte, 64)
	if _, err := handle.BulkTransfer(epBulkIn, buf, 500*time.Millisecond); err == nil {
		t.Error("read on stalled endpoint unexpectedly succeeded")
	}

	if err := handle.ClearFeature(reqTypeEndpointOut, featureEndpointHalt, epBulkIn); err != nil {
		t.Fatalf("ClearFeature(ENDPOINT_HALT): %v", err)
	}

	// ClearFeature only clears the device's own halt condition. The host-side
	// WinUSB/libusb pipe object tracks halt state independently (this is
	// exactly what libusb_clear_halt bundles automatically), so the pipe
	// also needs an explicit reset here or it can keep refusing transfers on
	// a pipe it still believes is halted, regardless of what the device now
	// reports.
	if err := handle.ClearHalt(epBulkIn); err != nil {
		t.Fatalf("ClearHalt: %v", err)
	}

	// Confirm the pipe actually works again after clearing the halt, not
	// just that the control requests themselves returned success.
	want := make([]byte, 64)
	for i := range want {
		want[i] = byte(i)
	}
	if _, err := handle.BulkTransfer(epBulkOut, want, 2*time.Second); err != nil {
		t.Fatalf("post-clear BulkTransfer OUT: %v", err)
	}
	if _, err := handle.BulkTransfer(epBulkIn, buf, 2*time.Second); err != nil {
		t.Fatalf("post-clear BulkTransfer IN: %v", err)
	}
}

// TestIsochronousCounter lives in isochronous_darwin_test.go and
// isochronous_other_test.go: go-usb's real isochronous entry points differ
// by platform today (macOS: IsochronousTransferIn/Out; Linux and Windows:
// NewIsochronousTransfer, and Windows' is currently stubbed), so there is no
// single portable call this file can make yet. See those files for why.
