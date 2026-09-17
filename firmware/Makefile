# Builds go-usb-jig's FX2 firmware with SDCC's mcs51 target.
#
# `make` produces firmware.ihx (Intel HEX), which a loader tool
# (fxload/cycfx2prog) writes into the FX2's RAM over USB. It does not flash
# anything itself -- there is no hardware step here, just compilation.

CC = sdcc
MODEL = --model-small
CFLAGS = -mmcs51 $(MODEL)

SRCS = main.c descriptors.c
OBJS = $(SRCS:.c=.rel)

firmware.ihx: $(OBJS)
	$(CC) $(CFLAGS) -o $@ $(OBJS)

%.rel: %.c fx2regs.h
	$(CC) $(CFLAGS) -c $< -o $@

clean:
	rm -f *.rel *.ihx *.lst *.sym *.asm *.lk *.map *.mem *.rst
