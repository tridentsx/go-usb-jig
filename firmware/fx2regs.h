/* FX2LP (CY7C68013A) register declarations.
 *
 * VERIFY AGAINST THE CYPRESS EZ-USB FX2LP TECHNICAL REFERENCE MANUAL BEFORE
 * FLASHING. These addresses and bit layouts are transcribed from memory, not
 * grepped out of the actual datasheet the way the macOS IOKit headers were
 * earlier in this project -- unlike those, they have not been cross-checked
 * against a canonical source. A wrong address here does not fail to compile;
 * it silently misconfigures the chip. Cross-reference every SFR and XDATA
 * address below against TRM chapters 8 (Endpoints) and 15 (Register Summary)
 * before relying on this for real hardware, or replace this header with
 * fx2lib's (a maintained, tested FX2 register/USB library) and keep only the
 * application logic in descriptors.c and main.c.
 */

#ifndef FX2REGS_H
#define FX2REGS_H

#include <8051.h> /* SDCC's mcs51 SFR declarations: P0-P3, standard 8051 core */

/* --- Core control (XDATA, not classic 8051 SFR space) --------------------- */
__xdata __at (0xE600) unsigned char CPUCS;   /* bit0 clock select, bit4 8051 reset */
__xdata __at (0xE601) unsigned char IFCONFIG;

/* --- USB control ------------------------------------------------------------ */
__xdata __at (0xE6A1) unsigned char USBCS;
__xdata __at (0xE68D) unsigned char EP0CS;
__xdata __at (0xE68E) unsigned char EP0BCH;
__xdata __at (0xE68F) unsigned char EP0BCL;
__xdata __at (0xE684) unsigned char EP1OUTCFG;
__xdata __at (0xE685) unsigned char EP1INCFG;
__xdata __at (0xE610) unsigned char EP2CFG;
__xdata __at (0xE611) unsigned char EP4CFG;
__xdata __at (0xE612) unsigned char EP6CFG;
__xdata __at (0xE613) unsigned char EP8CFG;
__xdata __at (0xE60D) unsigned char FIFORESET;

/* SETUPDAT: the eight-byte control transfer setup packet. */
__xdata __at (0xE6B8) unsigned char SETUPDAT[8];
#define bmRequestType SETUPDAT[0]
#define bRequest      SETUPDAT[1]
#define wValueL       SETUPDAT[2]
#define wValueH       SETUPDAT[3]
#define wIndexL       SETUPDAT[4]
#define wIndexH       SETUPDAT[5]
#define wLengthL      SETUPDAT[6]
#define wLengthH      SETUPDAT[7]

/* Per-endpoint byte counts and control, EP1/EP2/EP4/EP6/EP8. */
__xdata __at (0xE68B) unsigned char EP1OUTBC;
__xdata __at (0xE68C) unsigned char EP1INBC;
__xdata __at (0xE618) unsigned char EP2BCH;
__xdata __at (0xE619) unsigned char EP2BCL;
__xdata __at (0xE61A) unsigned char EP4BCH;
__xdata __at (0xE61B) unsigned char EP4BCL;
__xdata __at (0xE61C) unsigned char EP6BCH;
__xdata __at (0xE61D) unsigned char EP6BCL;
__xdata __at (0xE61E) unsigned char EP8BCH;
__xdata __at (0xE61F) unsigned char EP8BCL;

__xdata __at (0xE68A) unsigned char EP2468STAT; /* per-endpoint EMPTY/FULL flags */

/* USB interrupt request flags, polled rather than vectored -- this firmware
 * runs a polling main loop, not an interrupt-driven one, to keep the control
 * flow simple to reason about for a test jig. */
__xdata __at (0xE65F) unsigned char USBIRQ;
#define SUDAVbm 0x01 /* setup data available */
#define SUTOKbm 0x04 /* setup token received */

/* Endpoint FIFO buffers. Each is a 512- or 1024-byte window depending on
 * EPxCFG's SIZE bit and buffering mode; write/read sequentially, then
 * commit with the corresponding *BC register or INPKTEND/OUTPKTEND. */
__xdata __at (0xF000) unsigned char EP2FIFOBUF[1024];
__xdata __at (0xF400) unsigned char EP4FIFOBUF[512];
__xdata __at (0xF800) unsigned char EP6FIFOBUF[1024];
__xdata __at (0xFC00) unsigned char EP8FIFOBUF[512];

/* EP0 is fixed-size (64 bytes) and fixed-address, unlike EP2/4/6/8. */
__xdata __at (0xE740) unsigned char EP0BUF[64];

#endif
