/* USB descriptor tables for the go-usb-jig board.
 *
 * These are protocol-level byte layouts (USB 2.0 spec chapter 9), not
 * chip-specific register values, so they carry much less of the
 * "unverified against the real datasheet" risk fx2regs.h does -- the byte
 * layout of a configuration descriptor is the same on every USB device.
 *
 * Vendor-specific class (0xFF) throughout, deliberately: this is what keeps
 * the board from being claimed by any OS built-in class driver, unlike the
 * UVC webcam and USB audio devices that started this whole investigation.
 *
 * One interface, one alternate setting -- deliberately simpler than the
 * multi-alt-setting stock firmware this board shipped with, since a test
 * jig has no bandwidth-negotiation need. Endpoint map:
 *
 *   EP1 IN   interrupt   heartbeat counter
 *   EP2 OUT  bulk        loopback source
 *   EP6 IN   isochronous continuous counter pattern
 *   EP8 IN   bulk        loopback echo of what EP2 OUT received
 *   EP0      vendor requests: stall-an-endpoint, read/write internal RAM
 */

#include "fx2regs.h"

const unsigned char __code deviceDescriptor[] = {
	18,          /* bLength */
	0x01,        /* bDescriptorType: DEVICE */
	0x00, 0x02,  /* bcdUSB 2.00 */
	0xFF,        /* bDeviceClass: vendor-specific */
	0xFF,        /* bDeviceSubClass */
	0xFF,        /* bDeviceProtocol */
	64,          /* bMaxPacketSize0 */
	0x25, 0x09,  /* idVendor 0x0925 (Lakeview Research) */
	0x81, 0x38,  /* idProduct 0x3881 */
	0x00, 0x01,  /* bcdDevice 1.00 */
	0, 0, 0,     /* iManufacturer, iProduct, iSerialNumber: none */
	1,           /* bNumConfigurations */
};

const unsigned char __code configDescriptor[] = {
	/* Configuration descriptor */
	9, 0x02,
	32, 0x00,    /* wTotalLength = 32 (9 + 9 + 7*2 = 32) */
	1,           /* bNumInterfaces */
	1,           /* bConfigurationValue */
	0,           /* iConfiguration */
	0x80,        /* bmAttributes: bus powered */
	50,          /* bMaxPower: 100mA */

	/* Interface descriptor */
	9, 0x04,
	0,           /* bInterfaceNumber */
	0,           /* bAlternateSetting */
	4,           /* bNumEndpoints */
	0xFF, 0xFF, 0xFF, /* vendor-specific class/subclass/protocol */
	0,           /* iInterface */

	/* Endpoint: EP1 IN, interrupt, 64 bytes, 1ms interval */
	7, 0x05, 0x81, 0x03, 64, 0x00, 1,

	/* Endpoint: EP2 OUT, bulk, 512 bytes */
	7, 0x05, 0x02, 0x02, 0x00, 0x02, 0,

	/* Endpoint: EP6 IN, isochronous, 512 bytes, 1ms interval */
	7, 0x05, 0x86, 0x01, 0x00, 0x02, 1,

	/* Endpoint: EP8 IN, bulk, 512 bytes */
	7, 0x05, 0x88, 0x02, 0x00, 0x02, 0,
};
