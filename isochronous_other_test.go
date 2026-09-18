//go:build jig && !darwin

package jig

import "testing"

// epIsoIn is the isochronous IN endpoint on the go-usb-jig board.
// Direction bit is already set (0x80).
const epIsoIn = 0x88

// TestIsochronousCounter reads a burst of packets from EP8 IN, the
// free-running isochronous counter, via go-usb's portable
// NewIsochronousTransfer / Submit / Wait path (available on Linux and
// Windows). Individual packets may legitimately receive non-success statuses
// (USB 2.0 spec section 5.6.4: no retries, no delivery guarantee); this
// test only requires that at least some data arrives.
func TestIsochronousCounter(t *testing.T) {
	handle := openJig(t)

	const numPackets = 8
	const packetSize = 8

	it, err := handle.NewIsochronousTransfer(epIsoIn, numPackets, packetSize)
	if err != nil {
		t.Fatalf("NewIsochronousTransfer: %v", err)
	}

	if err := it.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := it.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if it.ActualLength() == 0 {
		t.Errorf("no isochronous data received across %d packets", numPackets)
	}
}
