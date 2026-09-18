//go:build jig

package jig

import (
	"bytes"
	"testing"
	"time"

	usb "github.com/kevmo314/go-usb"
)

// TestAsyncBulkLoopback exercises go-usb's portable AsyncTransfer surface
// (NewBulkTransfer/Fill/Submit/Wait/Buffer, satisfying AsyncTransferInterface
// on every platform -- see go-usb's api_contract_async.go) against the same
// EP2 OUT -> EP6 IN loopback TestBulkLoopback uses, to confirm the async path
// moves real data correctly, not just that it returns without error.
func TestAsyncBulkLoopback(t *testing.T) {
	handle := openJig(t)

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
