//go:build jig

package jig

import (
	"testing"
	"time"

	usb "github.com/tridentsx/go-usb"
)

// cpucsAddress and vendorLoad mirror cmd/flash/main.go's own constants: the
// EZ-USB 0xA0 RAM-download vendor request and the CPUCS register address
// that halts/resumes the 8051. Toggling CPUCS without writing any new code
// is enough on its own to force a real USB disconnect/reconnect (the
// firmware's RENUMERATE_UNCOND() call runs again from main() on resume),
// which is exactly the real event a hotplug watcher needs to see -- no
// physical unplug required, and no need to actually reflash anything.
const (
	cpucsAddress     = 0xE600
	vendorLoad       = 0xA0
	bmRequestTypeOut = 0x40
)

// triggerReenumeration halts and immediately resumes the jig's 8051 over a
// control transfer, without touching its resident firmware. Releasing the
// halt makes main() run RENUMERATE_UNCOND() again, producing one real
// DBT_DEVICEREMOVECOMPLETE followed by one real DBT_DEVICEARRIVAL from
// Windows -- a genuine hotplug event pair, not a simulated one.
func triggerReenumeration(t *testing.T, handle *usb.DeviceHandle) {
	t.Helper()
	if _, err := handle.ControlTransfer(bmRequestTypeOut, vendorLoad, cpucsAddress, 0, []byte{0x01}, 2*time.Second); err != nil {
		t.Fatalf("halting 8051 (CPUCS=1): %v", err)
	}
	if _, err := handle.ControlTransfer(bmRequestTypeOut, vendorLoad, cpucsAddress, 0, []byte{0x00}, 2*time.Second); err != nil {
		t.Fatalf("releasing 8051 (CPUCS=0): %v", err)
	}
}

// TestHotplugDetectsRealReenumeration registers go-usb's portable
// RegisterHotplugCallback for the jig's VID:PID, forces a real disconnect/
// reconnect by cycling the 8051's CPUCS register over a control transfer
// (see triggerReenumeration), and confirms a real HotplugEventLeft followed
// by a real HotplugEventArrived arrive for it -- proving the platform
// backend (see go-usb's hotplug_windows.go/hotplug_darwin.go/
// hotplug_linux.go) actually observes real OS-level device notifications,
// not just that it doesn't error out.
func TestHotplugDetectsRealReenumeration(t *testing.T) {
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

	type event struct {
		kind usb.HotplugEvent
		dev  *usb.Device
	}
	events := make(chan event, 16)

	handle, err := usb.RegisterHotplugCallback(jigVendorID, jigProductID, func(e usb.HotplugEvent, d *usb.Device) {
		events <- event{e, d}
	})
	if err != nil {
		t.Fatalf("RegisterHotplugCallback: %v", err)
	}
	defer handle.Deregister()

	// Registration delivers HotplugEventArrived for every matching device
	// already present (libusb's LIBUSB_HOTPLUG_ENUMERATE semantics) -- drain
	// that initial snapshot event for the board before triggering the real
	// one under test, so it isn't mistaken for it.
	select {
	case e := <-events:
		if e.kind != usb.HotplugEventArrived {
			t.Fatalf("initial snapshot event: got %v, want arrived", e.kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive the initial arrived snapshot for the already-connected board")
	}

	jigHandle, err := jig.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	triggerReenumeration(t, jigHandle)
	jigHandle.Close()

	var sawLeft, sawArrived bool
	deadline := time.After(5 * time.Second)
	for !sawLeft || !sawArrived {
		select {
		case e := <-events:
			if e.dev.Descriptor.VendorID != jigVendorID || e.dev.Descriptor.ProductID != jigProductID {
				t.Errorf("event for unexpected device %04x:%04x", e.dev.Descriptor.VendorID, e.dev.Descriptor.ProductID)
				continue
			}
			switch e.kind {
			case usb.HotplugEventLeft:
				if sawLeft {
					t.Error("saw a second 'left' event")
				}
				if sawArrived {
					t.Error("'left' arrived after 'arrived' -- events out of order")
				}
				sawLeft = true
			case usb.HotplugEventArrived:
				if !sawLeft {
					t.Error("'arrived' arrived before 'left' -- events out of order")
				}
				sawArrived = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for real hotplug events (sawLeft=%v sawArrived=%v)", sawLeft, sawArrived)
		}
	}

	// The "arrived" notification means Windows has registered the device
	// interface, not that WinUSB has finished re-binding to it: opening the
	// board again immediately after this test, in the same process, was
	// observed to fail ("The device does not recognize the command" from
	// SetInterfaceAltSetting) and even to make DeviceList briefly stop
	// seeing it at all. openJig's own 500ms sleep after SetConfiguration
	// exists for the same class of race; give the re-bind the same room to
	// settle here so later tests in the same run aren't affected by this
	// test's real disconnect/reconnect.
	time.Sleep(500 * time.Millisecond)
}
