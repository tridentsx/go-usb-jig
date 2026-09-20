//go:build jig

package jig

import (
	"bytes"
	"testing"
	"time"

	usb "github.com/tridentsx/go-usb"
)

// TestAsyncBulkLoopback exercises go-usb's portable AsyncTransfer surface
// (NewBulkTransfer/Fill/Submit/Wait/Buffer, satisfying AsyncTransferInterface
// on every platform -- see go-usb's api_contract_async.go) against the same
// EP2 OUT -> EP6 IN loopback TestBulkLoopback uses, to confirm the async path
// moves real data correctly, not just that it returns without error.
func TestAsyncBulkLoopback(t *testing.T) {
	handle := openJig(t)
	drainEP6IN(t, handle)

	want := make([]byte, 64)
	for i := range want {
		want[i] = byte(i)
	}

	out, err := handle.NewBulkTransfer(epBulkOut, len(want))
	if err != nil {
		t.Fatalf("NewBulkTransfer(out): %v", err)
	}
	if err := out.Fill(want); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if err := out.Submit(); err != nil {
		t.Fatalf("Submit(out): %v", err)
	}
	if err := out.Wait(); err != nil {
		t.Fatalf("Wait(out): %v", err)
	}

	in, err := handle.NewBulkTransfer(epBulkIn, 64)
	if err != nil {
		t.Fatalf("NewBulkTransfer(in): %v", err)
	}
	if err := in.Submit(); err != nil {
		t.Fatalf("Submit(in): %v", err)
	}
	if err := in.Wait(); err != nil {
		t.Fatalf("Wait(in): %v", err)
	}

	got := in.Buffer()
	if !bytes.Equal(got, want) {
		t.Errorf("echoed data does not match: got % x, want % x", got, want)
	}
}

// TestAsyncTransferSubmitReturnsPromptly confirms Submit queues the transfer
// and returns immediately, rather than blocking for however long the
// transfer itself takes -- the whole point of the async API over the
// synchronous BulkTransfer it wraps. It reads EP6 IN with nothing queued to
// echo (a fresh device, or one where nothing was just written to EP2 OUT),
// so the transfer legitimately takes up to its full timeout to resolve
// (empty forever -> ErrTimeout): Submit must still return in a small
// fraction of that time, and the caller must be able to do other work on the
// submitting goroutine before Wait blocks for the real result.
func TestAsyncTransferSubmitReturnsPromptly(t *testing.T) {
	handle := openJig(t)
	drainEP6IN(t, handle)

	const timeout = 2 * time.Second

	at, err := handle.NewBulkTransfer(epBulkIn, 64)
	if err != nil {
		t.Fatalf("NewBulkTransfer: %v", err)
	}
	at.SetTimeout(timeout)

	submitStart := time.Now()
	if err := at.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	submitElapsed := time.Since(submitStart)
	if submitElapsed > timeout/4 {
		t.Errorf("Submit took %v, expected it to return immediately (well under the %v transfer timeout)", submitElapsed, timeout)
	}

	// Other work the submitting goroutine can do while the transfer is
	// still in flight, proving Submit didn't block for it.
	sum := 0
	for i := 0; i < 1000; i++ {
		sum += i
	}
	if sum == 0 {
		t.Fatal("unreachable")
	}

	waitStart := time.Now()
	err = at.Wait()
	waitElapsed := time.Since(waitStart)
	if err != nil && err != usb.ErrTimeout {
		t.Fatalf("Wait: %v", err)
	}
	if waitElapsed < submitElapsed {
		t.Errorf("Wait returned in %v, faster than Submit's %v -- the real transfer time should be in Wait, not Submit", waitElapsed, submitElapsed)
	}
}

// drainEP6IN reads and discards real data left sitting in EP6 IN's
// quad buffer by an earlier test in the same process, until a read
// genuinely times out (nothing left). openJig's own SetInterfaceAltSetting
// reset already does this for TestBulkLoopback's benefit (see its comment),
// but that alone was not enough here: found on real hardware that
// TestAsyncTransferSubmitReturnsPromptly's "EP6 IN has nothing queued"
// assumption failed intermittently, resolving in real double-digit
// microseconds instead of timing out -- picking up a real, stale packet,
// not a bug in the transfer path itself. This establishes that
// precondition directly, by observation, instead of assuming it holds.
func drainEP6IN(t *testing.T, handle *usb.DeviceHandle) {
	t.Helper()

	for {
		drain, err := handle.NewBulkTransfer(epBulkIn, 64)
		if err != nil {
			t.Fatalf("NewBulkTransfer(drain): %v", err)
		}
		drain.SetTimeout(50 * time.Millisecond)
		if err := drain.Submit(); err != nil {
			t.Fatalf("Submit(drain): %v", err)
		}
		err = drain.Wait()
		if err == usb.ErrTimeout {
			return // genuinely empty now
		}
		if err != nil {
			t.Fatalf("Wait(drain): %v", err)
		}
		// Got real residual data; loop and check again -- the quad buffer
		// holds at most 4 packets, so this converges in a handful of tries.
	}
}

// TestCloseAfterAsyncTransferDoesNotDeadlock is a regression test for a
// real deadlock found on real Linux hardware while testing this exact
// board: go-usb's Close() cancels outstanding URBs via USBDEVFS_DISCARDURB
// specifically so a REAPURB call already blocked in the kernel unblocks --
// but once the reap loop has reaped everything and gone back to a *fresh*
// blocking REAPURB call with nothing outstanding, there is nothing left
// for Close to cancel, and that call never returns on its own. Fixed on
// go-usb's side by switching the reap loop to a non-blocking, polled
// USBDEVFS_REAPURBNDELAY; this test guards against it regressing.
//
// The key is that the transfer below has already completed (Wait
// returned) and been reaped by the time Close runs -- exactly the state
// that used to hang forever, as opposed to closing while something is
// still genuinely in flight.
func TestCloseAfterAsyncTransferDoesNotDeadlock(t *testing.T) {
	handle := openJig(t)

	at, err := handle.NewInterruptTransfer(epInterruptIn, 64)
	if err != nil {
		t.Fatalf("NewInterruptTransfer: %v", err)
	}
	at.SetTimeout(2 * time.Second)
	if err := at.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := at.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- handle.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return within 5s -- deadlocked (see this test's own doc comment for the real bug it guards against)")
	}
}
