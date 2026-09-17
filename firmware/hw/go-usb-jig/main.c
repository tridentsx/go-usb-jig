/*
 * go-usb-jig firmware, rebuilt on fx2lib after the from-scratch version
 * (see git history) never got past enumeration on real hardware.
 *
 * Structure closely follows sigrok-firmware-fx2lafw's fx2lafw.c, which is
 * proven working on this exact board (see README): fx2lib's setupdat.c
 * handles standard USB requests and calls back into the handle_* functions
 * below for the vendor-specific parts, RENUMERATE_UNCOND() forces the host
 * to actually notice new descriptors (the step the from-scratch firmware
 * was missing), and register access goes through fx2lib's fx2regs.h, which
 * is tested and shipped, not transcribed from memory.
 *
 * Endpoint map (see hw/go-usb-jig/dscr.a51): EP1 IN interrupt heartbeat,
 * EP2 OUT bulk loopback source, EP6 IN bulk echoes what EP2 OUT received,
 * EP8 IN isochronous free-running counter. Endpoint stall/clear-halt uses
 * the standard SET_FEATURE/CLEAR_FEATURE(ENDPOINT_HALT) requests, already
 * implemented by fx2lib's setupdat.c (see handle_set_feature/
 * handle_clear_feature there) -- no firmware change needed for that here.
 */

#include <fx2regs.h>
#include <fx2macros.h>
#include <autovector.h>
#include <delay.h>
#include <setupdat.h>
#include <eputils.h>

/* eputils.h's RESETFIFO/RESETTOGGLE need SYNCDELAY defined by the caller;
 * see its header comment. Matches fx2lafw.h's definition exactly. */
#define SYNCDELAY() SYNCDELAY4

volatile __bit got_sud;
volatile WORD heartbeat_counter = 0;
static BYTE heartbeat = 0;
static BYTE isoCounter = 0;

/* Vendor request numbers for the scratch-RAM read/write test. Chosen well
 * clear of the standard request range (0-12) and of 0xA0 (EZ-USB's own
 * built-in RAM download command, handled by the chip before firmware ever
 * sees it) and 0xA1-0xAF (reserved alongside it per fx2lib's setupdat.h). */
#define VR_READ_RAM  0xB6
#define VR_WRITE_RAM 0xB7
#define RAM_SIZE 256
static __xdata BYTE scratch_ram[RAM_SIZE];

BOOL handle_vendorcommand(BYTE cmd)
{
	BYTE offset = SETUPDAT[2]; /* wValueL */
	WORD count = SETUP_LENGTH();

	/* offset is a BYTE (0-255) and RAM_SIZE is 256, so offset is always a
	 * valid index; only the length needs clamping to stay in bounds. */
	switch (cmd) {
	case VR_READ_RAM:
		if (count > (WORD)(RAM_SIZE - offset))
			count = RAM_SIZE - offset;
		writeep0(scratch_ram + offset, count);
		return TRUE;

	case VR_WRITE_RAM:
		if (count > (WORD)(RAM_SIZE - offset))
			count = RAM_SIZE - offset;
		readep0(scratch_ram + offset, count);
		return TRUE;

	default:
		return FALSE;
	}
}

BOOL handle_get_interface(BYTE ifc, BYTE *alt_ifc)
{
	if (ifc != 0)
		return FALSE;
	*alt_ifc = 0;
	return TRUE;
}

BOOL handle_set_interface(BYTE ifc, BYTE alt_ifc)
{
	if (ifc != 0 || alt_ifc != 0)
		return FALSE;

	/* Reset data toggles and FIFOs for the endpoints in this interface.
	 * Isochronous endpoints don't use data toggling (TRM 5.4), so EP8 only
	 * needs its FIFO reset, not RESETTOGGLE. */
	RESETTOGGLE(0x81);
	RESETTOGGLE(0x02);
	RESETTOGGLE(0x86);
	RESETFIFO(0x02);
	RESETFIFO(0x06);
	RESETFIFO(0x08);

	return TRUE;
}

BYTE handle_get_configuration(void)
{
	return 1;
}

BOOL handle_set_configuration(BYTE cfg)
{
	return (cfg == 1) ? TRUE : FALSE;
}

void sudav_isr(void) __interrupt(SUDAV_ISR)
{
	got_sud = TRUE;
	CLEAR_SUDAV();
}

void usbreset_isr(void) __interrupt(USBRESET_ISR)
{
	handle_hispeed(FALSE);
	CLEAR_USBRESET();
}

void hispeed_isr(void) __interrupt(HISPEED_ISR)
{
	handle_hispeed(TRUE);
	CLEAR_HISPEED();
}

static void setup_endpoints(void)
{
	/* EPxCFG's TYPE field (bits 5:4) uses the same 2-bit encoding as a USB
	 * endpoint descriptor's transfer type: 01=isochronous, 10=bulk,
	 * 11=interrupt (confirmed against sigrok-firmware-fx2lafw's
	 * fx2lafw.c setup_endpoints(), proven working on this exact board).
	 * Bulk is bit5 set, bit4 clear -- NOT both bits, which is interrupt.
	 *
	 * EP1 IN: interrupt. EP1 has no SIZE/BUF bits; its buffer is a fixed
	 * 64 bytes per the TRM. */
	EP1INCFG = bmVALID | bmBIT5 | bmBIT4; /* TYPE=interrupt (11). */
	SYNCDELAY();

	/* EP2 OUT: bulk, 1024-byte buffer, quad-buffered. */
	EP2CFG = bmVALID | bmBIT5 | bmBIT3; /* TYPE=bulk (10), SIZE=1024, quad-buffered (BUF=00). */
	SYNCDELAY();

	/* EP6 IN: bulk, 1024-byte buffer, quad-buffered. */
	EP6CFG = bmVALID | bmDIR | bmBIT5 | bmBIT3;
	SYNCDELAY();

	/* EP4 is unused. */
	EP4CFG &= ~bmVALID;
	SYNCDELAY();

	/* EP8 IN: isochronous. Per TRM 8.4, EP8 is always fixed at 512 bytes,
	 * double-buffered -- it has no SIZE/BUF bits, only VALID/DIRECTION/
	 * TYPE. TYPE=01 (bit4 set, bit5 clear) is isochronous, confirmed
	 * against the real TRM's EPxCFG bit table (Section 8.4), not guessed
	 * from the bulk/interrupt pattern above. */
	EP8CFG = bmVALID | bmDIR | bmBIT4; /* TYPE=isochronous (01). */
	SYNCDELAY();

	/* One packet per microframe at high speed (TRM 8.6.2.2); this is
	 * also the hardware default, but set it explicitly. Doesn't affect
	 * full-speed operation, which is always fixed at one packet/frame. */
	EP8ISOINPKTS = 1;
	SYNCDELAY();

	/* Disable AUTOIN (TRM 8.4/9.3.7): with it left at its uninitialized
	 * reset state, the packet committed to the host was sized from
	 * EP8AUTOINLENH:L instead of the EP8BCH:L write below, causing a
	 * kIOReturnOverrun on every packet when the two disagreed. This
	 * firmware commits packets manually via EP8BCL, exactly like EP1/EP6
	 * below, so AUTOIN must be off. */
	EP8FIFOCFG = 0;
	SYNCDELAY();

	RESETFIFO(0x02);
	RESETFIFO(0x06);
	RESETFIFO(0x08);
}

void main(void)
{
	got_sud = FALSE;

	RENUMERATE_UNCOND();

	SETCPUFREQ(CLK_48M);

	USE_USB_INTS();
	ENABLE_SUDAV();
	ENABLE_HISPEED();
	ENABLE_USBRESET();

	EA = 1; /* Global (8051) interrupt enable. */

	setup_endpoints();

	while (1) {
		if (got_sud) {
			handle_setupdata();
			got_sud = FALSE;
		}

		/* EP1 IN heartbeat: send one byte whenever the buffer is free. */
		if (!(EP1INCS & bmEPBUSY)) {
			EP1INBUF[0] = heartbeat++;
			SYNCDELAY();
			EP1INBC = 1;
		}

		/* Bulk loopback: if EP2 OUT has data and EP6 IN is free, copy it
		 * across. EP2468STAT's bits report each endpoint's empty/full
		 * state; see fx2regs.h. */
		if (!(EP2468STAT & bmEP2EMPTY) && !(EP2468STAT & bmEP6FULL)) {
			BYTE i, count = EP2BCL;
			for (i = 0; i < count; i++)
				EP6FIFOBUF[i] = EP2FIFOBUF[i];
			SYNCDELAY();
			EP6BCH = 0;
			SYNCDELAY();
			EP6BCL = count;
			SYNCDELAY();
			OUTPKTEND = 0x02 | 0x80; /* Free the EP2 OUT buffer we consumed. */
		}

		/* EP8 IN isochronous: send one byte whenever a buffer is free.
		 * EP8 is double-buffered, so it uses the EP2468STAT FULL/EMPTY
		 * check (like EP6 above), not a single BUSY bit (like EP1). */
		if (!(EP2468STAT & bmEP8FULL)) {
			EP8FIFOBUF[0] = isoCounter++;
			SYNCDELAY();
			EP8BCH = 0;
			SYNCDELAY();
			EP8BCL = 1;
		}
	}
}
