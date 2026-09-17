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
;; Endpoint map, one interface, one alternate setting:
;;   EP1 IN   interrupt  free-running heartbeat byte
;;   EP2 OUT  bulk       loopback source
;;   EP6 IN   bulk       echoes whatever EP2 OUT last received
;;
;; Isochronous and vendor RAM/stall commands are deliberately left out of
;; this first rebuild on fx2lib, to get a solid, verified bulk/interrupt
;; baseline on real hardware before adding back the more complex pieces the
;; from-scratch firmware never got working. See main.c.

.include "common.inc"

.module DEV_DSCR

DSCR_DEVICE_TYPE	= 1
DSCR_CONFIG_TYPE	= 2
DSCR_STRING_TYPE	= 3
DSCR_INTERFACE_TYPE	= 4
DSCR_ENDPOINT_TYPE	= 5
DSCR_DEVQUAL_TYPE	= 6

DSCR_INTERFACE_LEN	= 9
DSCR_ENDPOINT_LEN	= 7

ENDPOINT_TYPE_CONTROL	= 0
ENDPOINT_TYPE_ISO	= 1
ENDPOINT_TYPE_BULK	= 2
ENDPOINT_TYPE_INT	= 3

VID = 0x2509	; idVendor 0x0925 (Lakeview Research), byte-swapped for .dw
PID = 0x8138	; idProduct 0x3881, byte-swapped for .dw

.globl _dev_dscr, _dev_qual_dscr, _highspd_dscr, _fullspd_dscr, _dev_strings, _dev_strings_end
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
	.db	1			; Number of interfaces
	.db	1			; Configuration number
	.db	0			; Configuration string (none)
	.db	0x80			; Attributes (bus powered, no wakeup)
	.db	0x32			; Max. power (100mA)
highspd_dscr_end:

	.db	DSCR_INTERFACE_LEN
	.db	DSCR_INTERFACE_TYPE
	.db	0			; Interface index
	.db	0			; Alternate setting index
	.db	3			; Number of endpoints
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
	.db	1
	.db	1
	.db	0
	.db	0x80
	.db	0x32
fullspd_dscr_end:

	.db	DSCR_INTERFACE_LEN
	.db	DSCR_INTERFACE_TYPE
	.db	0
	.db	0
	.db	3
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

fullspd_dscr_realend:

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
