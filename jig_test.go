//go:build jig

// Hardware-gated tests against a physical go-usb-jig FX2 board.
//
// Excluded from normal `go test ./...` by the jig build tag, since these
// need real hardware attached: run with `go test -tags jig ./...`. Each test
// skips cleanly, rather than failing, if the board isn't found -- a run
// without hardware attached should say so, not look like a broken test.

package jig

import (
	"bytes"
	"testing"
	"time"

	usb "github.com/kevmo314/go-usb"
)

// jigVendorID and jigProductID identify the board. See firmware/descriptors.c
// for the device descriptor these come from.
const (
	jigVendorID  = 0x0925
	jigProductID = 0x3881
)

// Endpoint addresses and vendor request numbers, matching
// firmware/descriptors.c and firmware/main.c exactly. Keep these in sync by
// hand; nothing generates them from the firmware source.
const (
	epInterruptIn = 0x81
	epBulkOut     = 0x02
	epIsoIn       = 0x86
	epBulkIn      = 0x88

	vrStallEP  = 0x01
	vrReadRAM  = 0x02
	vrWriteRAM = 0x03
)

// openJig finds and opens the board and claims its one interface, which has
// only one alternate setting -- it exposes every test endpoint from the
// start, unlike the multi-alt-setting stock firmware this board originally
// shipped with. It skips the test if the board isn't attached.
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

	if cfg, err := handle.GetConfiguration(); err != nil {
		t.Fatalf("GetConfiguration: %v", err)
	} else if cfg == 0 {
		if err := handle.SetConfiguration(1); err != nil {
			t.Fatalf("SetConfiguration(1): %v", err)
		}
	}

	if err := handle.ClaimInterface(0); err != nil {
		t.Fatalf("ClaimInterface(0): %v", err)
	}
	t.Cleanup(func() { handle.ReleaseInterface(0) })

	return handle
}

// TestBulkLoopback writes a known pattern to EP2 OUT and reads it back from
// EP8 IN, which the firmware echoes it to (see echoBulk in firmware/main.c).
// Unlike the isochronous test against the stock firmware, this checks actual
// data correctness, not just that the mechanism completed.
func TestBulkLoopback(t *testing.T) {
	handle := openJig(t)

	want := make([]byte, 512)
	for i := range want {
		want[i] = byte(i)
	}

	if _, err := handle.BulkTransfer(epBulkOut, want, 2*time.Second); err != nil {
		t.Fatalf("BulkTransfer OUT: %v", err)
	}

	got := make([]byte, 512)
	n, err := handle.BulkTransfer(epBulkIn, got, 2*time.Second)
	if err != nil {
		t.Fatalf("BulkTransfer IN: %v", err)
	}
	got = got[:n]

	if !bytes.Equal(got, want) {
		t.Errorf("echoed data does not match: got % x, want % x", got, want)
	}
}

// TestInterruptHeartbeat reads EP1 IN twice and checks the byte advanced,
// confirming the firmware's free-running heartbeat (see fillInterrupt in
// firmware/main.c) rather than just that a read completed.
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

// TestIsochronousCounter reads several isochronous packets from EP6 IN and
// checks the counter pattern the firmware streams (see fillIsochronous in
// firmware/main.c) for internal consistency: each byte should be one more
// than the last, wrapping at 256. This is the real-data follow-up to the
// stock-firmware test, which only proved the async mechanism itself worked
// (every packet reported kIOUSBNotSent1Err, since nothing was driving data).
func TestIsochronousCounter(t *testing.T) {
	handle := openJig(t)

	transfer, err := handle.IsochronousTransferIn(epIsoIn&0x0F, 8, 512)
	if err != nil {
		t.Fatalf("IsochronousTransferIn: %v", err)
	}
	if err := transfer.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if transfer.ActualLength() == 0 {
		t.Fatal("no data received; is the firmware actually filling EP6?")
	}

	buf := transfer.Buffer()
	for i := 1; i < len(buf); i++ {
		if buf[i] != byte(buf[i-1]+1) {
			t.Errorf("byte %d = %d, want %d (previous byte + 1)", i, buf[i], buf[i-1]+1)
			break
		}
	}
}

// TestVendorStallAndClearHalt stalls EP8 via the board's vendor request,
// confirms a transfer on it fails, then confirms ClearHalt actually clears
// the stall -- exercising ClearHalt against a real stall condition rather
// than a hypothetical one.
func TestVendorStallAndClearHalt(t *testing.T) {
	handle := openJig(t)

	// bmRequestType 0x40: host-to-device, vendor, device recipient.
	if _, err := handle.ControlTransfer(0x40, vrStallEP, 0, epBulkIn, nil, 2*time.Second); err != nil {
		t.Fatalf("vendor stall request: %v", err)
	}

	buf := make([]byte, 512)
	if _, err := handle.BulkTransfer(epBulkIn, buf, 2*time.Second); err == nil {
		t.Fatal("BulkTransfer succeeded on a pipe the firmware was told to stall")
	}

	if err := handle.ClearHalt(epBulkIn); err != nil {
		t.Fatalf("ClearHalt: %v", err)
	}

	// A fresh write/read pair, since the stalled read above consumed whatever
	// the firmware last echoed.
	want := []byte{1, 2, 3, 4}
	if _, err := handle.BulkTransfer(epBulkOut, want, 2*time.Second); err != nil {
		t.Fatalf("BulkTransfer OUT after ClearHalt: %v", err)
	}
	if _, err := handle.BulkTransfer(epBulkIn, buf, 2*time.Second); err != nil {
		t.Fatalf("BulkTransfer IN after ClearHalt: %v", err)
	}
}
