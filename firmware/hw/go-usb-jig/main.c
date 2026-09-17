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
 * EP2 OUT bulk loopback source, EP6 IN bulk echoes what EP2 OUT received.
 * Isochronous and vendor RAM/stall commands are deliberately left for a
 * follow-up once this baseline is confirmed on the bench.
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

BOOL handle_vendorcommand(BYTE cmd)
{
	(void)cmd;
	return FALSE;
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

	/* Reset data toggles and FIFOs for the endpoints in this interface. */
	RESETTOGGLE(0x81);
	RESETTOGGLE(0x02);
	RESETTOGGLE(0x86);
	RESETFIFO(0x02);
	RESETFIFO(0x06);

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

	/* EP4 and EP8 are unused. */
	EP4CFG &= ~bmVALID;
	SYNCDELAY();
	EP8CFG &= ~bmVALID;
	SYNCDELAY();

	RESETFIFO(0x02);
	RESETFIFO(0x06);
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
	}
}
