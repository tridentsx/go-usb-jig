//go:build jig && (darwin || windows)

// Hardware-gated test against go-usb-jig's dummy HID interface (interface
// 1, EP4 IN) -- see firmware/hw/go-usb-jig/dscr.a51 and main.c's HID
// handling. Added because no real off-the-shelf HID instrument was
// available to validate go-usb's HID transport (particularly the new macOS
// IOHIDDevice backend) against real hardware.
//
// darwin/windows only: IsHID/GetFeatureReport/HIDReportLengths etc. are
// deliberately platform-specific in go-usb, not part of its enforced
// cross-platform contract -- Linux's HID story is raw transfers after
// DetachKernelDriver instead (see go-usb's README), a fundamentally
// different shape of test this file doesn't attempt to cover.

package jig

import (
	"bytes"
	"testing"
	"time"

	usb "github.com/tridentsx/go-usb"
)

const hidInterfaceNum = 1

// openJigHID finds and opens the board and claims its HID interface,
// mirroring openJig in jig_test.go but for interface 1 instead of 0.
func openJigHID(t *testing.T) *usb.DeviceHandle {
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

	if err := handle.ClaimInterface(hidInterfaceNum); err != nil {
		t.Fatalf("ClaimInterface(%d): %v (is the flashed firmware built with the HID interface? see dscr.a51)", hidInterfaceNum, err)
	}
	t.Cleanup(func() { handle.ReleaseInterface(hidInterfaceNum) })

	if !handle.IsHID() {
		t.Fatal("ClaimInterface succeeded but IsHID() is false -- claimed the wrong interface, or the HID fallback did not trigger")
	}

	return handle
}

// TestHIDInputCounter reads a few Input reports over InterruptTransfer and
// checks the free-running counter in byte 1 is actually advancing, the same
// kind of liveness check TestBulkLoopback and the isochronous test give the
// other transfer types.
//
// This is the one path Apple documents as unreliable if read the other way
// (GetInputReport's on-demand poll) -- InterruptTransfer instead exercises
// the async report-callback pump, which is the reliable path on macOS. See
// go-usb's README, "Bulk streams are Linux-only for now" section's sibling
// HID note, and hid_darwin.go's getInputReport comment.
func TestHIDInputCounter(t *testing.T) {
	handle := openJigHID(t)

	in, out, feature, err := handle.HIDReportLengths(0x84)
	if err != nil {
		t.Fatalf("HIDReportLengths: %v", err)
	}
	if in != 9 || out != 9 || feature != 9 {
		t.Fatalf("report lengths = input=%d output=%d feature=%d, want 9/9/9 (1 report-ID byte + 8 data bytes)", in, out, feature)
	}

	var last byte
	var sawAdvance bool
	for i := 0; i < 20; i++ {
		buf := make([]byte, 9)
		n, err := handle.InterruptTransfer(0x84, buf, 2*time.Second)
		if err != nil {
			t.Fatalf("InterruptTransfer read %d: %v", i, err)
		}
		if n != 9 {
			t.Fatalf("InterruptTransfer read %d: got %d bytes, want 9", i, n)
		}
		if buf[0] != 1 {
			t.Fatalf("InterruptTransfer read %d: report ID = %d, want 1", i, buf[0])
		}
		if i > 0 && buf[1] != last {
			sawAdvance = true
		}
		last = buf[1]
	}
	if !sawAdvance {
		t.Error("counter byte never changed across 20 reads")
	}
}

// TestHIDFeatureReportRoundTrip writes a Feature report via SetFeatureReport
// and reads it back via GetFeatureReport, exercising the report-level
// control path (IOHIDDeviceSetReport/GetReport on macOS, HidD_SetFeature/
// GetFeature on Windows, a raw control transfer on Linux).
func TestHIDFeatureReportRoundTrip(t *testing.T) {
	handle := openJigHID(t)

	want := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04}
	if err := handle.SetFeatureReport(1, want); err != nil {
		t.Fatalf("SetFeatureReport: %v", err)
	}

	got := make([]byte, 8)
	n, err := handle.GetFeatureReport(1, got)
	if err != nil {
		t.Fatalf("GetFeatureReport: %v", err)
	}
	if n != len(want) || !bytes.Equal(got[:n], want) {
		t.Errorf("got % x, want % x", got[:n], want)
	}
}

// TestHIDOutputReport writes an Output report via SetOutputReport. The
// firmware stores it (see hid_output_report in main.c) but this device
// doesn't expose a way to read it back through the portable API -- HID
// GET_REPORT(Output) is what the firmware itself supports, but go-usb has
// no public method for it (Windows' hid.dll has no equivalent call either),
// so this only checks that the write itself succeeds without error.
func TestHIDOutputReport(t *testing.T) {
	handle := openJigHID(t)

	if err := handle.SetOutputReport(1, []byte{1, 2, 3, 4, 5, 6, 7, 8}); err != nil {
		t.Fatalf("SetOutputReport: %v", err)
	}
}

// TestHIDFlushQueue exercises FlushHIDQueue, which should never error just
// because the queue happens to be empty.
func TestHIDFlushQueue(t *testing.T) {
	handle := openJigHID(t)

	if err := handle.FlushHIDQueue(); err != nil {
		t.Fatalf("FlushHIDQueue: %v", err)
	}
}
