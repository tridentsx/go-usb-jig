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

/* HID test interface (interface 1, EP4 IN) -- a dummy HID device for
 * testing go-usb's HID transport end to end, since no real off-the-shelf
 * HID instrument was available. See dscr.a51's hid_report_dscr for the
 * report layout: one Report ID (1), 8-byte Input/Output/Feature reports
 * each, on a vendor-defined usage page. */
#define HID_INTERFACE_NUM 1

static BYTE hid_input_counter = 0;
static __xdata BYTE hid_output_report[8];
static __xdata BYTE hid_feature_report[8];

/* HID class request codes (HID spec 7.2), and the report-type values in
 * wValueH for GET_REPORT/SET_REPORT. */
#define HID_GET_REPORT 0x01
#define HID_SET_REPORT 0x09
#define HID_REPORT_TYPE_INPUT   1
#define HID_REPORT_TYPE_OUTPUT  2
#define HID_REPORT_TYPE_FEATURE 3

/* is_hid_class_request reports whether this SETUP packet is a HID class
 * request (GET_REPORT/SET_REPORT) targeting our HID interface, rather than
 * whatever standard or vendor request the numerically colliding bRequest
 * value (see handle_set_configuration and handle_vendorcommand below)
 * would otherwise mean. bmRequestType's Type field (bits 6:5) is 01 for
 * Class, and Recipient (bits 4:0) is 1 for Interface; wIndexL (SETUPDAT[4])
 * carries the target interface number for an interface-recipient request. */
static BOOL is_hid_class_request(void)
{
	return (SETUPDAT[0] & 0x60) == 0x20
	    && (SETUPDAT[0] & 0x1F) == 0x01
	    && SETUPDAT[4] == HID_INTERFACE_NUM;
}

/* handle_hid_get_report serves HID GET_REPORT (wValueH = report type,
 * wValueL = report ID -- ignored, since this device only ever declares
 * report ID 1 for each type). Reached from handle_vendorcommand, because
 * GET_REPORT's bRequest (0x01) numerically collides with the standard
 * CLEAR_FEATURE request that fx2lib's setupdat.c dispatches on bRequest
 * alone; CLEAR_FEATURE's own handler falls through to handle_vendorcommand
 * for any bmRequestType it doesn't itself recognize, which is where this is
 * caught instead. */
static BOOL handle_hid_get_report(void)
{
	BYTE report_type = SETUPDAT[3];
	WORD count = SETUP_LENGTH();

	switch (report_type) {
	case HID_REPORT_TYPE_INPUT: {
		__xdata BYTE snapshot[8];
		BYTE i;
		snapshot[0] = hid_input_counter;
		for (i = 1; i < sizeof(snapshot); i++)
			snapshot[i] = 0;
		if (count > sizeof(snapshot))
			count = sizeof(snapshot);
		writeep0(snapshot, count);
		return TRUE;
	}
	case HID_REPORT_TYPE_OUTPUT:
		if (count > sizeof(hid_output_report))
			count = sizeof(hid_output_report);
		writeep0(hid_output_report, count);
		return TRUE;
	case HID_REPORT_TYPE_FEATURE:
		if (count > sizeof(hid_feature_report))
			count = sizeof(hid_feature_report);
		writeep0(hid_feature_report, count);
		return TRUE;
	default:
		return FALSE;
	}
}

/* handle_hid_set_report serves HID SET_REPORT. Reached from
 * handle_set_configuration, for the same kind of bRequest collision as
 * GET_REPORT above: SET_REPORT is 0x09, the same value as the standard
 * SET_CONFIGURATION request. */
static BOOL handle_hid_set_report(void)
{
	BYTE report_type = SETUPDAT[3];
	WORD count = SETUP_LENGTH();

	switch (report_type) {
	case HID_REPORT_TYPE_OUTPUT:
		if (count > sizeof(hid_output_report))
			count = sizeof(hid_output_report);
		readep0(hid_output_report, count);
		return TRUE;
	case HID_REPORT_TYPE_FEATURE:
		if (count > sizeof(hid_feature_report))
			count = sizeof(hid_feature_report);
		readep0(hid_feature_report, count);
		return TRUE;
	default:
		return FALSE;
	}
}

/* Debug counters for diagnosing the real bulk-transfer timeout seen from
 * both macOS (IOKit) and Windows (WinUSB): incremented in the main loop, so
 * a host can ask "did the device's own USB engine ever actually see
 * anything land in EP2 OUT" independent of whatever the host-side transfer
 * call reported. If this stays at 0 across a failed WritePipe attempt, the
 * device-side SIE never saw the OUT token at all, which points away from
 * firmware (the main loop can't be blamed for consuming something that
 * never arrived) and toward the physical link (hub, cable, hardware). If it
 * increments, the device did see the data and the failure is in how/
 * whether that success gets communicated back to the host.
 */
static WORD ep2_seen_count = 0;
static WORD ep6_committed_count = 0;

/* Vendor request numbers for the scratch-RAM read/write test. Chosen well
 * clear of the standard request range (0-12) and of 0xA0 (EZ-USB's own
 * built-in RAM download command, handled by the chip before firmware ever
 * sees it) and 0xA1-0xAF (reserved alongside it per fx2lib's setupdat.h). */
#define VR_READ_RAM  0xB6
#define VR_WRITE_RAM 0xB7

/* VR_READ_REGS: live register/counter snapshot for diagnosing the bulk
 * timeout. IN, no data stage input -- see reg_snapshot below for the
 * layout. Chosen well clear of VR_READ_RAM/VR_WRITE_RAM and the reserved
 * ranges noted above. */
#define VR_READ_REGS 0xB8

/* VR_RESET_BULK: actively drains EP2 OUT's quad buffer and resets both
 * bulk endpoints' FIFOs/toggles. No data stage.
 *
 * A bare RESETFIFO (what handle_set_interface already does on every
 * ClaimInterface, and what a software reflash's fresh setup_endpoints()
 * call also does) does NOT clear an OUT endpoint's already-buffered,
 * unconsumed packets -- confirmed on real hardware: EP2CS still reported
 * NPAK=4/FULL immediately after both a RESETFIFO and a full software
 * reflash. RESETFIFO resets FIFO pointers, not the SIE's count of
 * already-accepted packets sitting in the buffer. The actual fix is to
 * commit-and-discard each stuck packet via OUTPKTEND, the same call the
 * main loop already uses to release a packet it copied -- the hardware
 * doesn't care whether the data was read first, only that OUTPKTEND was
 * called once per buffered packet. */
#define VR_RESET_BULK 0xB9

#define RAM_SIZE 256
static __xdata BYTE scratch_ram[RAM_SIZE];

/* reg_snapshot is what VR_READ_REGS returns, built fresh on every request
 * rather than kept live, since EP2CS/EP6CS/EP2468STAT/EP2FIFOFLGS/
 * EP6FIFOFLGS need reading at the moment of the request, not cached. Field
 * order and meaning (see fx2regs.h for the real bit names):
 *   [0]   EP2CS       bmNPAK (6:4), bmEPFULL (3), bmEPEMPTY (2), bmEPSTALL (0)
 *   [1]   EP6CS       same bit layout as EP2CS
 *   [2]   EP2468STAT  bmEP2FULL/EMPTY (1:0), bmEP6FULL/EMPTY (5:4)
 *   [3]   EP2CFG
 *   [4]   EP6CFG
 *   [5]   USBCS
 *   [6]   EP2FIFOFLGS
 *   [7]   EP6FIFOFLGS
 *   [8:9] ep2_seen_count, low byte first (how many times the main loop has
 *         seen EP2468STAT report EP2 non-empty, i.e. real data arrived)
 *   [10:11] ep6_committed_count, low byte first (how many times the main
 *         loop has committed a copied packet to EP6 IN)
 */
static __xdata BYTE reg_snapshot[12];

static void fill_reg_snapshot(void)
{
	reg_snapshot[0] = EP2CS;
	reg_snapshot[1] = EP6CS;
	reg_snapshot[2] = EP2468STAT;
	reg_snapshot[3] = EP2CFG;
	reg_snapshot[4] = EP6CFG;
	reg_snapshot[5] = USBCS;
	reg_snapshot[6] = EP2FIFOFLGS;
	reg_snapshot[7] = EP6FIFOFLGS;
	reg_snapshot[8] = (BYTE)(ep2_seen_count & 0xff);
	reg_snapshot[9] = (BYTE)(ep2_seen_count >> 8);
	reg_snapshot[10] = (BYTE)(ep6_committed_count & 0xff);
	reg_snapshot[11] = (BYTE)(ep6_committed_count >> 8);
}

BOOL handle_vendorcommand(BYTE cmd)
{
	BYTE offset = SETUPDAT[2]; /* wValueL */
	WORD count = SETUP_LENGTH();

	/* HID GET_REPORT (0x01) numerically collides with the standard
	 * CLEAR_FEATURE request; see is_hid_class_request's comment and
	 * handle_clear_feature in fx2lib/lib/setupdat.c, whose own
	 * unrecognized-bmRequestType fallthrough is what actually reaches this
	 * function for that request. Checked ahead of the switch below, which
	 * is keyed on the colliding bRequest value alone. */
	if (cmd == HID_GET_REPORT && is_hid_class_request())
		return handle_hid_get_report();

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

	case VR_READ_REGS:
		fill_reg_snapshot();
		if (count > sizeof(reg_snapshot))
			count = sizeof(reg_snapshot);
		writeep0(reg_snapshot, count);
		return TRUE;

	case VR_RESET_BULK: {
		BYTE i;
		/* Quad-buffered: unconditionally discard exactly 4 packets,
		 * generous delay between each, rather than trusting
		 * EP2468STAT's EMPTY bit to have caught up between calls (it
		 * did not, in practice: it can read back EMPTY while EP2CS
		 * still separately reports FULL/NPAK=4 immediately after). */
		for (i = 0; i < 4; i++) {
			OUTPKTEND = 0x02 | 0x80;
			SYNCDELAY();
			SYNCDELAY();
			SYNCDELAY();
			SYNCDELAY();
		}
		return TRUE;
	}

	default:
		return FALSE;
	}
}

BOOL handle_get_interface(BYTE ifc, BYTE *alt_ifc)
{
	if (ifc != 0 && ifc != HID_INTERFACE_NUM)
		return FALSE;
	*alt_ifc = 0;
	return TRUE;
}

BOOL handle_set_interface(BYTE ifc, BYTE alt_ifc)
{
	if (alt_ifc != 0)
		return FALSE;

	if (ifc == HID_INTERFACE_NUM) {
		RESETTOGGLE(0x84);
		RESETFIFO(0x04);
		return TRUE;
	}
	if (ifc != 0)
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
	/* HID SET_REPORT (0x09) numerically collides with the standard
	 * SET_CONFIGURATION request that fx2lib/lib/setupdat.c's
	 * handle_setupdata dispatches straight to this function -- unlike
	 * GET_REPORT above, there is no unrecognized-bmRequestType fallthrough
	 * in that path, so the check has to happen here, the one hook this
	 * specific collision actually reaches. See is_hid_class_request's
	 * comment. */
	if (is_hid_class_request())
		return handle_hid_set_report();

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
	/* OUTPKTEND (used below in main(), and by VR_RESET_BULK) has no effect
	 * at all unless REVCTL.0 (ENH_PKT) is set -- confirmed against the
	 * real TRM's OUTPKTEND register reference, not the more casual usage
	 * examples elsewhere in the same manual, which don't mention this
	 * gate. Every real TRM example that calls OUTPKTEND sets REVCTL = 0x03
	 * first (both ENH_PKT bits). Without this, every OUTPKTEND call in
	 * this file was a silent no-op -- found by adding a diagnostic vendor
	 * command and observing EP2CS never change across repeated attempts
	 * to discard its buffered packets. */
	REVCTL = 0x03;
	SYNCDELAY();

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

	/* EP4 IN: interrupt, for the HID test interface (interface 1). Same
	 * EPxCFG bit layout as EP2/EP6 above (TRM: EP2CFG/EP4CFG/EP6CFG/EP8CFG
	 * are the same register at consecutive addresses), SIZE bit (bit3)
	 * left clear for the default (512-byte) buffer -- this endpoint only
	 * ever commits 9 bytes at a time, so the smallest available size would
	 * do, but there's no meaningful cost to leaving it at the default the
	 * other endpoints already use. */
	EP4CFG = bmVALID | bmDIR | bmBIT5 | bmBIT4; /* TYPE=interrupt (11). */
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
	RESETFIFO(0x04);
	RESETFIFO(0x06);
	RESETFIFO(0x08);

	/* Drain any packets left stuck in EP2 OUT's quad buffer from a
	 * previous firmware run. RESETFIFO above only resets FIFO pointers --
	 * it does not clear the SIE's count of already-accepted, unconsumed
	 * packets (confirmed on real hardware: EP2CS kept reporting NPAK=4/
	 * FULL immediately after RESETFIFO). Only a real power cycle resets
	 * that SIE-level state on its own; a software reflash (cmd/flash's
	 * 0xA0 command) does not, since it only halts and reloads the 8051
	 * CPU. Discarding via OUTPKTEND, the same mechanism VR_RESET_BULK
	 * uses on demand, is what actually clears it, so do it unconditionally
	 * on every boot rather than relying on the host to notice and ask. */
	{
		BYTE drain;
		for (drain = 0; drain < 4; drain++) {
			OUTPKTEND = 0x02 | 0x80;
			SYNCDELAY();
			SYNCDELAY();
			SYNCDELAY();
			SYNCDELAY();
		}
	}
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
		if (!(EP2468STAT & bmEP2EMPTY)) {
			ep2_seen_count++;

			if (!(EP2468STAT & bmEP6FULL)) {
				BYTE i, count = EP2BCL;
				for (i = 0; i < count; i++)
					EP6FIFOBUF[i] = EP2FIFOBUF[i];
				SYNCDELAY();
				EP6BCH = 0;
				SYNCDELAY();
				EP6BCL = count;
				SYNCDELAY();
				OUTPKTEND = 0x02 | 0x80; /* Free the EP2 OUT buffer we consumed. */
				/* Let OUTPKTEND settle before the next iteration re-reads
				 * EP2468STAT: VR_RESET_BULK's drain loop already waits four
				 * SYNCDELAYs after each OUTPKTEND for exactly this reason.
				 * Without it here, ep2_seen_count/ep6_committed_count were
				 * observed to over-count a single host write (e.g. 3
				 * instead of 1), consistent with EP2468STAT's EP2EMPTY bit
				 * not yet reflecting the just-freed buffer on the very next
				 * loop pass, so the same packet's now-stale FIFO contents
				 * get copied and committed again. */
				SYNCDELAY();
				SYNCDELAY();
				SYNCDELAY();
				SYNCDELAY();
				ep6_committed_count++;
			}
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

		/* EP4 IN: the HID test interface's Input report, one Report ID
		 * (1) byte followed by 8 data bytes, a free-running counter in
		 * the first data byte -- InterruptTransfer on the host side
		 * should see this incrementing across repeated reads, the same
		 * kind of liveness check the EP1/EP8 counters give the other
		 * transfer types. */
		if (!(EP2468STAT & bmEP4FULL)) {
			BYTE i;
			EP4FIFOBUF[0] = 1; /* Report ID */
			EP4FIFOBUF[1] = hid_input_counter++;
			for (i = 2; i < 9; i++)
				EP4FIFOBUF[i] = 0;
			SYNCDELAY();
			EP4BCH = 0;
			SYNCDELAY();
			EP4BCL = 9;
		}
	}
}
