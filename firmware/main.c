/* go-usb-jig firmware: a polling-loop USB device exercising bulk, interrupt,
 * isochronous and vendor control transfers for testing github.com/kevmo314/go-usb.
 *
 * DRAFT, NOT YET FLASHED OR VERIFIED AGAINST REAL HARDWARE. The SETUP
 * handshake sequence, the stall mechanism and the FIFO commit sequence below
 * are marked VERIFY where this was written from memory rather than a
 * checked source; see fx2regs.h's header comment. Treat a first bring-up as
 * an iterative bench session, not a single flash-and-done step -- exactly
 * how the pipeRef and frame-margin bugs on the Go side were found, by
 * running it and reading back what actually happened rather than trusting
 * the code review alone.
 */

#include "fx2regs.h"

extern const unsigned char __code deviceDescriptor[18];
extern const unsigned char __code configDescriptor[32];

/* Vendor request numbers, bRequest when bmRequestType selects Vendor. */
#define VR_STALL_EP  0x01 /* wIndex = endpoint address to stall */
#define VR_READ_RAM  0x02 /* wValue = offset, wLength = count; IN */
#define VR_WRITE_RAM 0x03 /* wValue = offset; OUT */

/* Internal scratch RAM the vendor read/write requests expose, and the
 * isochronous/bulk fill patterns write into for inspection. */
#define RAM_SIZE 256
static unsigned char __xdata ram[RAM_SIZE];

/* isoCounter/heartbeat track the free-running patterns EP6 and EP1 stream,
 * so a host-side test can verify the sequence has no gaps. */
static unsigned char isoCounter;
static unsigned char heartbeat;

/* --- EP0 helpers ------------------------------------------------------------ */

/* sendEP0 copies up to 64 bytes into EP0BUF and arms the IN transaction.
 * VERIFY: the exact register(s) that both set the byte count and signal
 * "data is ready" for EP0 IN on FX2LP -- this assumes writing EP0BCH/EP0BCL
 * (high byte first) is itself the arm, which is the common pattern in FX2
 * example firmware, but confirm against TRM chapter 8 before trusting it.
 */
static void sendEP0(const unsigned char *data, unsigned char len) {
	unsigned char i;
	for (i = 0; i < len; i++) {
		EP0BUF[i] = data[i];
	}
	EP0BCH = 0;
	EP0BCL = len;
}

/* stallEP0 rejects the current control request. VERIFY: EP0CS bit position
 * for STALL on FX2LP.
 */
static void stallEP0(void) {
	EP0CS |= 0x01;
}

/* ackEP0 acknowledges a zero-data-stage request (SET_CONFIGURATION,
 * SET_INTERFACE, the vendor write side after data received, etc). VERIFY:
 * FX2LP's EP0CS has a HSNAK (handshake) bit that must be set to complete a
 * zero-length status stage; confirm the bit position.
 */
static void ackEP0(void) {
	EP0CS |= 0x02;
}

/* --- Vendor requests --------------------------------------------------------- */

/* stallEndpoint stalls pipe endpoint (wIndex from the request) so a
 * host-side test can exercise ClearHalt against a real stall condition.
 * VERIFY: FX2LP's mechanism for stalling EP1/2/4/6/8 specifically -- unlike
 * EP0CS.STALL above, the non-zero endpoints may use a different register or
 * a per-endpoint control bit; this is the least-verified part of this
 * firmware and should be checked first on the bench.
 */
static void stallEndpoint(unsigned char endpoint) {
	(void)endpoint; /* TODO: implement once the stall mechanism is confirmed */
}

static void handleVendorRequest(void) {
	switch (bRequest) {
	case VR_STALL_EP:
		stallEndpoint(wIndexL);
		ackEP0();
		break;

	case VR_READ_RAM: {
		unsigned char offset = wValueL;
		unsigned char count = wLengthL;
		if (count > 64) count = 64;
		if ((unsigned int)offset + count > RAM_SIZE) count = RAM_SIZE - offset;
		sendEP0(ram + offset, count);
		break;
	}

	case VR_WRITE_RAM:
		/* VERIFY: reading the OUT data stage on EP0 for a vendor write needs
		 * its own SUTOK/EP0BC handshake, not implemented in this draft.
		 * The bulk/isochronous/interrupt paths below are the priority; this
		 * one is lower value (control-transfer round-trip testing) and can
		 * follow once those are confirmed working.
		 */
		ackEP0();
		break;

	default:
		stallEP0();
		break;
	}
}

/* --- Standard requests ------------------------------------------------------- */

static void handleStandardRequest(void) {
	switch (bRequest) {
	case 0x06: /* GET_DESCRIPTOR */
		switch (wValueH) {
		case 0x01: /* DEVICE */
			sendEP0(deviceDescriptor, sizeof(deviceDescriptor));
			break;
		case 0x02: /* CONFIGURATION */
			sendEP0(configDescriptor, sizeof(configDescriptor) < wLengthL ? sizeof(configDescriptor) : wLengthL);
			break;
		default:
			stallEP0();
			break;
		}
		break;

	case 0x09: /* SET_CONFIGURATION */
	case 0x0B: /* SET_INTERFACE */
		ackEP0();
		break;

	case 0x08: { /* GET_CONFIGURATION */
		static const unsigned char __code one[] = {1};
		sendEP0(one, 1);
		break;
	}

	default:
		stallEP0();
		break;
	}
}

/* --- Endpoint fill routines --------------------------------------------------- */

/* fillIsochronous keeps EP6 IN loaded with an incrementing counter whenever
 * its buffer is free, so a host-side isochronous read gets a sequence it can
 * check for gaps or corruption rather than opaque data.
 * VERIFY: the exact EP2468STAT bit for "EP6 IN buffer available", and
 * whether committing the packet is via EP6BCH/EP6BCL alone or also needs an
 * explicit INPKTEND for a short packet.
 */
static void fillIsochronous(void) {
	unsigned int i;
	for (i = 0; i < 512; i++) {
		EP6FIFOBUF[i] = isoCounter++;
	}
	EP6BCH = 0x02;
	EP6BCL = 0x00; /* 512 = 0x0200 */
}

/* fillInterrupt keeps EP1 IN loaded with a one-byte heartbeat. */
static void fillInterrupt(void) {
	EP0BUF[0] = heartbeat++; /* placeholder buffer; VERIFY EP1IN's real FIFO address */
	EP1INBC = 1;
}

/* echoBulk copies whatever the host last sent on EP2 OUT back out on EP8 IN,
 * a minimal loopback that lets a host-side test check bulk data integrity
 * without needing the firmware to generate its own pattern.
 * VERIFY: the OUT-side handshake (detecting EP2 has new data, and releasing
 * its buffer afterward) in EP2468STAT / relevant OUTPKTEND-style register.
 */
static void echoBulk(void) {
	unsigned char count = EP2BCL;
	unsigned int i;
	for (i = 0; i < count; i++) {
		EP8FIFOBUF[i] = EP2FIFOBUF[i];
	}
	EP8BCH = 0;
	EP8BCL = count;
}

/* --- Main -------------------------------------------------------------------- */

void main(void) {
	CPUCS = 0x00;
	IFCONFIG = 0x00; /* VERIFY: correct value to disable the GPIF/ports this jig doesn't use */

	EP1INCFG = 0xA0;  /* VALID, TYPE=interrupt -- VERIFY exact bit encoding */
	EP2CFG = 0x00;    /* VALID, OUT, bulk -- VERIFY exact bit encoding */
	EP6CFG = 0xCC;    /* VALID, IN, isochronous -- VERIFY exact bit encoding */
	EP8CFG = 0xE0;    /* VALID, IN, bulk -- VERIFY exact bit encoding */
	FIFORESET = 0x80;

	for (;;) {
		if (USBIRQ & SUDAVbm) {
			USBIRQ = SUDAVbm; /* clear by writing the bit -- VERIFY */

			if ((bmRequestType & 0x60) == 0x40) {
				handleVendorRequest();
			} else {
				handleStandardRequest();
			}
		}

		fillIsochronous();
		fillInterrupt();
		echoBulk();
	}
}
