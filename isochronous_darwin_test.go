//go:build jig && darwin

package jig

import "testing"

// epIsoIn is a bare endpoint number; IsochronousTransferIn ORs in the
// direction bit itself.
const epIsoIn = 0x08

// TestIsochronousCounter reads a burst of packets from EP8 IN, the
// free-running isochronous counter, via go-usb's macOS-specific
// IsochronousTransferIn/Wait (there is no portable one-shot isochronous
// call today -- see isochronous_other_test.go). Real isochronous transfers
// have no retries and no guarantee every microframe is serviced (USB 2.0
// spec section 5.6.4), and this firmware fills the endpoint from a plain
// polling loop with no SOF synchronization, so individual packets
// legitimately see non-success statuses (observed: kIOReturnOverrun early
// in a burst, kIOReturnUnderrun later) -- this only checks that some real
// data comes through overall, not that every packet is clean.
func TestIsochronousCounter(t *testing.T) {
	handle := openJig(t)

	const numPackets = 8
	const packetSize = 8

	it, err := handle.IsochronousTransferIn(epIsoIn, numPackets, packetSize)
	if err != nil {
		t.Fatalf("IsochronousTransferIn: %v", err)
	}
	if err := it.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if it.ActualLength() == 0 {
		t.Errorf("no isochronous data received across %d packets", numPackets)
	}
}
