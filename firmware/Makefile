# Builds go-usb-jig's FX2 firmware on top of the real fx2lib (vendored in
# fx2lib/, from sigrok-firmware-fx2lafw's bundled copy -- see fx2lib/README
# and COPYING for its license), using the exact SDCC invocations and link
# flags sigrok-firmware-fx2lafw's own Makefile.am uses for its
# hw/cypress-fx2 variant, which is proven working on this project's test
# board.
#
# `make` produces firmware.ihx (Intel HEX). It does not flash anything --
# that's cmd/flash, a separate step, over USB.

SDCC = sdcc
SDAS = sdas8051
SDAR = sdar

CFLAGS = -mmcs51 -Iinclude -Ifx2lib/include
LINKFLAGS = -mmcs51 --code-size 0x1c00 --xram-size 0x0200 --xram-loc 0x1c00 \
	-Wl"-b DSCR_AREA=0x1e00" -Wl"-b INT2JT=0x1f00"

FX2LIB_OBJS = fx2lib/lib/delay.rel fx2lib/lib/eputils.rel fx2lib/lib/gpif.rel \
	fx2lib/lib/i2c.rel fx2lib/lib/int4av.rel fx2lib/lib/serial.rel \
	fx2lib/lib/setupdat.rel fx2lib/lib/usbav.rel

FX2LIB_INT_SRCS = $(wildcard fx2lib/lib/interrupts/*.c)
FX2LIB_INT_OBJS = $(FX2LIB_INT_SRCS:.c=.rel)

.PHONY: all clean

all: firmware.ihx

firmware.ihx: hw/go-usb-jig/dscr.rel hw/go-usb-jig/main.rel fx2lib/lib/fx2.lib fx2lib/lib/interrupts/ints.lib
	$(SDCC) $(LINKFLAGS) -o $@ hw/go-usb-jig/dscr.rel hw/go-usb-jig/main.rel \
		fx2lib/lib/fx2.lib fx2lib/lib/interrupts/ints.lib

hw/go-usb-jig/main.rel: hw/go-usb-jig/main.c
	$(SDCC) $(CFLAGS) -c $< -o $@

hw/go-usb-jig/dscr.rel: hw/go-usb-jig/dscr.a51 include/common.inc
	cd hw/go-usb-jig && $(SDAS) -logs -I../../include dscr.a51

fx2lib/lib/fx2.lib: $(FX2LIB_OBJS)
	$(SDAR) -rc $@ $(FX2LIB_OBJS)

fx2lib/lib/interrupts/ints.lib: $(FX2LIB_INT_OBJS)
	$(SDAR) -rc $@ $(FX2LIB_INT_OBJS)

fx2lib/lib/%.rel: fx2lib/lib/%.c
	$(SDCC) -mmcs51 -Ifx2lib/include -c $< -o $@

fx2lib/lib/interrupts/%.rel: fx2lib/lib/interrupts/%.c
	$(SDCC) -mmcs51 -Ifx2lib/include -c $< -o $@

fx2lib/lib/usbav.rel: fx2lib/lib/usbav.a51
	cd fx2lib/lib && $(SDAS) -logs usbav.a51

fx2lib/lib/int4av.rel: fx2lib/lib/int4av.a51
	cd fx2lib/lib && $(SDAS) -logs int4av.a51

clean:
	find . -name '*.rel' -o -name '*.lib' -o -name '*.asm' -o -name '*.lst' -o -name '*.sym' -o -name '*.ihx' -o -name '*.lk' -o -name '*.map' -o -name '*.mem' -o -name '*.rst' | xargs rm -f
