//go:build jig

package jig

import (
	"testing"
	"time"
)

// vrReadRegs is the firmware's diagnostic vendor request (see main.c's
// VR_READ_REGS / fill_reg_snapshot) added while chasing a real bulk-transfer
// timeout seen from both macOS and Windows: it returns live EP2/EP6
// register state plus two free-running counters, so a host can tell whether
// the device's own USB engine ever actually saw data land in EP2 OUT,
// independent of what either host's driver stack reports.
const vrReadRegs = 0xB8

// TestDumpRegisters is a diagnostic, not a pass/fail check: it always
// passes and just logs the current register/counter snapshot, so it's
// useful to run on demand (`go test -tags jig -run TestDumpRegisters -v .`)
// before and after reproducing the bulk timeout, on any platform.
func TestDumpRegisters(t *testing.T) {
	handle := openJig(t)

	buf := make([]byte, 12)
	n, err := handle.ControlTransfer(0xC0, vrReadRegs, 0, 0, buf, 2*time.Second)
	if err != nil {
		t.Fatalf("VR_READ_REGS: %v", err)
	}
	b := buf[:n]
	if len(b) < 12 {
		t.Fatalf("VR_READ_REGS returned %d bytes, want 12", len(b))
	}

	ep2Seen := uint16(b[8]) | uint16(b[9])<<8
	ep6Committed := uint16(b[10]) | uint16(b[11])<<8

	t.Logf("EP2CS=%#02x EP6CS=%#02x EP2468STAT=%#02x EP2CFG=%#02x EP6CFG=%#02x "+
		"USBCS=%#02x EP2FIFOFLGS=%#02x EP6FIFOFLGS=%#02x ep2_seen=%d ep6_committed=%d",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], ep2Seen, ep6Committed)

	// EP2CS/EP6CS bit 3 (bmEPFULL) and bits 6:4 (bmNPAK) together tell you
	// whether the quad buffer is stuck full. See main.c's fill_reg_snapshot
	// comment for the full bit layout.
	const bmEPFULL = 0x08
	if b[0]&bmEPFULL != 0 {
		t.Logf("EP2 OUT quad buffer is FULL (NPAK=%d) -- a new bulk write has nowhere to go", (b[0]>>4)&0x7)
	}
	if b[1]&bmEPFULL != 0 {
		t.Logf("EP6 IN quad buffer is FULL (NPAK=%d) -- nothing has read it since it filled", (b[1]>>4)&0x7)
	}
}
