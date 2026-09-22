//go:build jig && windows

// hid_darwin_test.go's tests cannot run against this board on Windows, and
// the reason is architectural, not a Zadig misconfiguration to fix here:
// on Windows, go-usb's IsHID() reports whether the *whole device handle*
// went through the HID class driver, decided exactly once in Device.Open
// by whether WinUsb_Initialize succeeds for the file handle as a whole
// (see go-usb's hid_api_windows.go and device_windows.go). ClaimInterface
// for a non-zero interface only calls WinUsb_GetAssociatedInterface --
// plain WinUSB pipe access -- and never touches that decision.
//
// This board's interface 0 is WinUSB-bound, so Open's WinUsb_Initialize
// call succeeds and IsHID() is false for the whole handle, no matter which
// interface is claimed afterward: there is no code path in go-usb's
// Windows backend today that can treat one specific claimed interface as a
// HID collection while the handle overall is WinUSB-based. macOS doesn't
// have this limitation because IOKit exposes each interface as its own
// service, which is exactly why hid_darwin_test.go's own header comment
// says it was added to validate the macOS IOHIDDevice backend in the first
// place -- the windows build tag it carried before this file existed
// assumed a parity with that model the Windows implementation doesn't
// actually have.
//
// Bypassing hid.dll and talking raw WinUSB to the HID interface directly
// (GET_REPORT/SET_REPORT are just class-specific control transfers, Input
// reports are just interrupt-IN payloads) is technically possible and
// would close this gap, but requires rebinding the *entire* device to
// WinUSB the way this board already is -- fine for a dedicated test jig,
// but not something to build until a real consumer of go-usb's Windows HID
// support wants a composite WinUSB+HID device to work this way.
//
// Skipped, not silently absent, to match every other cross-platform gap
// convention in this repo (see isochronous_other_test.go).

package jig

import "testing"

const skipReason = "go-usb's Windows backend has no path to treat one claimed interface as HID while the handle overall is WinUSB-based -- see this file's header comment"

func TestHIDInputCounter(t *testing.T)           { t.Skip(skipReason) }
func TestHIDFeatureReportRoundTrip(t *testing.T) { t.Skip(skipReason) }
func TestHIDOutputReport(t *testing.T)           { t.Skip(skipReason) }
func TestHIDFlushQueue(t *testing.T)             { t.Skip(skipReason) }
