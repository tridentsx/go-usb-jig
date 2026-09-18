;;
;; USB descriptors for the go-usb-jig test board.
;;
;; Adapted from sigrok-firmware-fx2lafw's dscr.inc/dscr.a51 pattern (GPLv2+),
;; itself the reference this file's structure and macros come from -- see
;; ../../fx2lib/README and COPYING for fx2lib's own license.
;;
;; Vendor-specific class throughout (0xFF), deliberately, so no OS built-in
;; class driver ever claims this device -- see the top-level README.
;;
;; Endpoint map, two interfaces:
;;   Interface 0 (vendor-specific, one alternate setting):
;;     EP1 IN   interrupt     free-running heartbeat byte
;;     EP2 OUT  bulk          loopback source
;;     EP6 IN   bulk          echoes whatever EP2 OUT last received
;;     EP8 IN   isochronous   free-running counter byte, one packet/microframe
;;   Interface 1 (HID, one alternate setting) -- a dummy HID device for
;;   testing this library's HID transport end to end, since no real
;;   off-the-shelf HID instrument was available. Vendor-defined usage page
;;   (0xff00), so this library's own keyboard/pointer exclusion policy never
;;   filters it out; see hid_report_dscr below and main.c's HID handling.
;;     EP4 IN   interrupt     one Report ID (1): 8-byte input report, a
;;                            free-running counter
;;
;; Endpoint stall/clear-halt is exercised with the standard
;; SET_FEATURE/CLEAR_FEATURE(ENDPOINT_HALT) requests, which fx2lib's
;; setupdat.c already implements against real hardware -- no custom vendor
;; command needed for that. A custom vendor command (VR_READ_RAM/
;; VR_WRITE_RAM) exercises control transfers with a real data stage; see
;; main.c.

.include "common.inc"

.module DEV_DSCR

DSCR_DEVICE_TYPE	= 1
DSCR_CONFIG_TYPE	= 2
DSCR_STRING_TYPE	= 3
DSCR_INTERFACE_TYPE	= 4
DSCR_ENDPOINT_TYPE	= 5
DSCR_DEVQUAL_TYPE	= 6
DSCR_HID_TYPE		= 0x21
DSCR_HID_REPORT_TYPE	= 0x22

DSCR_INTERFACE_LEN	= 9
DSCR_ENDPOINT_LEN	= 7

ENDPOINT_TYPE_CONTROL	= 0
ENDPOINT_TYPE_ISO	= 1
ENDPOINT_TYPE_BULK	= 2
ENDPOINT_TYPE_INT	= 3

VID = 0x2509	; idVendor 0x0925 (Lakeview Research), byte-swapped for .dw
PID = 0x8138	; idProduct 0x3881, byte-swapped for .dw

.globl _dev_dscr, _dev_qual_dscr, _highspd_dscr, _fullspd_dscr, _dev_strings, _dev_strings_end
.globl _hid_report_dscr, _hid_report_dscr_end
.area DSCR_AREA (CODE)

; -----------------------------------------------------------------------------
; Device descriptor
; -----------------------------------------------------------------------------
_dev_dscr:
	.db	dev_dscr_end - _dev_dscr
	.db	DSCR_DEVICE_TYPE
	.dw	0x0002			; USB 2.0
	.db	0xff			; Class (vendor specific)
	.db	0xff			; Subclass (vendor specific)
	.db	0xff			; Protocol (vendor specific)
	.db	64			; Max. EP0 packet size
	.dw	VID
	.dw	PID
	.dw	0x0100			; Product version (0.01)
	.db	1			; Manufacturer string index
	.db	2			; Product string index
	.db	0			; Serial number string index (none)
	.db	1			; Number of configurations
dev_dscr_end:

; -----------------------------------------------------------------------------
; Device qualifier (for "other device speed")
; -----------------------------------------------------------------------------
_dev_qual_dscr:
	.db	dev_qualdscr_end - _dev_qual_dscr
	.db	DSCR_DEVQUAL_TYPE
	.dw	0x0002
	.db	0xff
	.db	0xff
	.db	0xff
	.db	64
	.db	1
	.db	0
dev_qualdscr_end:

; -----------------------------------------------------------------------------
; High-Speed configuration descriptor
; -----------------------------------------------------------------------------
_highspd_dscr:
	.db	highspd_dscr_end - _highspd_dscr
	.db	DSCR_CONFIG_TYPE
	.db	(highspd_dscr_realend - _highspd_dscr) % 256
	.db	(highspd_dscr_realend - _highspd_dscr) / 256
	.db	2			; Number of interfaces
	.db	1			; Configuration number
	.db	0			; Configuration string (none)
	.db	0x80			; Attributes (bus powered, no wakeup)
	.db	0x32			; Max. power (100mA)
highspd_dscr_end:

	.db	DSCR_INTERFACE_LEN
	.db	DSCR_INTERFACE_TYPE
	.db	0			; Interface index
	.db	0			; Alternate setting index
	.db	4			; Number of endpoints
	.db	0xff
	.db	0xff
	.db	0xff
	.db	0

	; EP1 IN, interrupt
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x81
	.db	ENDPOINT_TYPE_INT
	.db	0x40			; 64 bytes
	.db	0x00
	.db	0x08			; bInterval: high speed encodes this as
					; 2^(bInterval-1) microframes, so 8 = 128
					; microframes = 16ms; bInterval=1 (every
					; microframe) was needlessly aggressive.

	; EP2 OUT, bulk
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x02
	.db	ENDPOINT_TYPE_BULK
	.db	0x00			; 512 bytes
	.db	0x02
	.db	0x00

	; EP6 IN, bulk
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x86
	.db	ENDPOINT_TYPE_BULK
	.db	0x00			; 512 bytes
	.db	0x02
	.db	0x00

	; EP8 IN, isochronous. EP8 is fixed at 512 bytes double-buffered by the
	; hardware (TRM 8.4), but the descriptor's wMaxPacketSize can declare
	; any smaller value; 1 matches what main.c's fill loop actually commits
	; per microframe (EP8BCL=1) -- declaring 64 here while firmware only
	; ever sends 1 byte caused stale-buffer garbage and underruns on the
	; host side, since the host expects a full 64-byte packet every
	; microframe. bInterval=1 (one microframe) matches EP8ISOINPKTS=1 (one
	; packet/microframe) in main.c.
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x88
	.db	ENDPOINT_TYPE_ISO
	.db	0x08			; 8 bytes (declared larger than the 1 byte actually sent, to test if the overrun is size-specific)
	.db	0x00
	.db	0x01

	; Interface 1: HID (dummy device for testing this library's HID
	; transport -- see the header comment above and main.c).
	.db	DSCR_INTERFACE_LEN
	.db	DSCR_INTERFACE_TYPE
	.db	1			; Interface index
	.db	0			; Alternate setting index
	.db	1			; Number of endpoints
	.db	0x03			; Class (HID)
	.db	0x00			; Subclass (none -- not boot keyboard/mouse)
	.db	0x00			; Protocol (none)
	.db	0

	; HID descriptor (functional descriptor, embedded in the config
	; descriptor per the HID spec; the report descriptor it points at is
	; fetched separately, see hid_report_dscr and setupdat.c's
	; handle_get_descriptor).
	.db	9			; bLength
	.db	DSCR_HID_TYPE
	.dw	0x1101			; bcdHID 1.11, byte-swapped for .dw
	.db	0			; bCountryCode (none)
	.db	1			; bNumDescriptors
	.db	DSCR_HID_REPORT_TYPE
	.db	(_hid_report_dscr_end - _hid_report_dscr) % 256
	.db	(_hid_report_dscr_end - _hid_report_dscr) / 256

	; EP4 IN, interrupt
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x84
	.db	ENDPOINT_TYPE_INT
	.db	0x09			; 9 bytes (1 report ID + 8 data bytes)
	.db	0x00
	.db	0x08			; bInterval: same encoding/value as EP1 IN above.

highspd_dscr_realend:

	.even

; -----------------------------------------------------------------------------
; Full-Speed configuration descriptor
; -----------------------------------------------------------------------------
_fullspd_dscr:
	.db	fullspd_dscr_end - _fullspd_dscr
	.db	DSCR_CONFIG_TYPE
	.db	(fullspd_dscr_realend - _fullspd_dscr) % 256
	.db	(fullspd_dscr_realend - _fullspd_dscr) / 256
	.db	2
	.db	1
	.db	0
	.db	0x80
	.db	0x32
fullspd_dscr_end:

	.db	DSCR_INTERFACE_LEN
	.db	DSCR_INTERFACE_TYPE
	.db	0
	.db	0
	.db	4
	.db	0xff
	.db	0xff
	.db	0xff
	.db	0

	; EP1 IN, interrupt
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x81
	.db	ENDPOINT_TYPE_INT
	.db	0x40
	.db	0x00
	.db	0x08			; bInterval, full speed: a direct frame
					; count (1ms units), so 8 = 8ms.

	; EP2 OUT, bulk
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x02
	.db	ENDPOINT_TYPE_BULK
	.db	0x40			; 64 bytes (full speed max)
	.db	0x00
	.db	0x00

	; EP6 IN, bulk
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x86
	.db	ENDPOINT_TYPE_BULK
	.db	0x40
	.db	0x00
	.db	0x00

	; EP8 IN, isochronous. Full-speed isochronous bInterval must be 1 (one
	; frame) per the USB 2.0 spec -- unlike interrupt, it isn't a free
	; choice. 1 byte, matching the high-speed block; see its comment.
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x88
	.db	ENDPOINT_TYPE_ISO
	.db	0x01
	.db	0x00
	.db	0x01

	; Interface 1: HID (dummy device for testing this library's HID
	; transport -- see the header comment above and main.c).
	.db	DSCR_INTERFACE_LEN
	.db	DSCR_INTERFACE_TYPE
	.db	1
	.db	0
	.db	1
	.db	0x03
	.db	0x00
	.db	0x00
	.db	0

	.db	9
	.db	DSCR_HID_TYPE
	.dw	0x1101			; bcdHID 1.11, byte-swapped for .dw
	.db	0
	.db	1
	.db	DSCR_HID_REPORT_TYPE
	.db	(_hid_report_dscr_end - _hid_report_dscr) % 256
	.db	(_hid_report_dscr_end - _hid_report_dscr) / 256

	; EP4 IN, interrupt
	.db	DSCR_ENDPOINT_LEN
	.db	DSCR_ENDPOINT_TYPE
	.db	0x84
	.db	ENDPOINT_TYPE_INT
	.db	0x09			; 9 bytes (1 report ID + 8 data bytes)
	.db	0x00
	.db	0x08			; bInterval, full speed: 8ms, matching EP1 IN.

fullspd_dscr_realend:

	.even

; -----------------------------------------------------------------------------
; HID Report descriptor for interface 1 (see main.c's HID handling)
; -----------------------------------------------------------------------------
;
; Vendor-defined usage page (0xff00), deliberately: this library's own
; hidCollectionUsable policy (see hid.go) excludes pointer/keyboard/digitizer
; usage pages, and a vendor page is exactly what real HID-based test
; equipment uses. One Report ID (1), with an 8-byte Input, Output and Feature
; report each -- enough to exercise InterruptTransfer (input, via the async
; report pump), GetFeatureReport/SetFeatureReport, GetInputReport and
; SetOutputReport all against real hardware. Each report's declared max
; length as the OS reports it is 9 bytes (1 report-ID byte + 8 data bytes),
; matching this library's convention that InterruptTransfer's payload
; includes the leading report-ID byte.
_hid_report_dscr:
	.db	0x06, 0x00, 0xff	; Usage Page (Vendor Defined 0xff00)
	.db	0x09, 0x01		; Usage (1)
	.db	0xa1, 0x01		; Collection (Application)
	.db	0x85, 0x01		;   Report ID (1)
	.db	0x15, 0x00		;   Logical Minimum (0)
	.db	0x26, 0xff, 0x00	;   Logical Maximum (255)
	.db	0x75, 0x08		;   Report Size (8)
	.db	0x95, 0x08		;   Report Count (8)
	.db	0x09, 0x02		;   Usage (2)
	.db	0x81, 0x02		;   Input (Data,Var,Abs)
	.db	0x09, 0x03		;   Usage (3)
	.db	0x91, 0x02		;   Output (Data,Var,Abs)
	.db	0x09, 0x04		;   Usage (4)
	.db	0xb1, 0x02		;   Feature (Data,Var,Abs)
	.db	0xc0			; End Collection
_hid_report_dscr_end:

	.even

; -----------------------------------------------------------------------------
; Strings
; -----------------------------------------------------------------------------
_dev_strings:

string_descriptor_lang 0 0x0409

string_descriptor_a 1,^"go-usb-jig"
string_descriptor_a 2,^"Test Board"

_dev_strings_end:
	.dw	0x0000
