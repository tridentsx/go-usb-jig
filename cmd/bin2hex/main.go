// bin2hex converts a raw binary firmware image into Intel HEX, for testing
// cmd/flash's Intel-HEX code path against a known-good image such as one of
// libsigrok's bundled fx2lafw-*.fw files (which are raw binaries, loaded by
// libsigrok's own ezusb.c as one contiguous image from address 0, not
// Intel HEX). That cross-check is what isolated go-usb-jig's own firmware
// bug from the loader: see the README's "Update, first real flash attempts"
// section.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <in.bin> <out.ihx>\n", os.Args[0])
		os.Exit(2)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	out, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer out.Close()

	const bytesPerLine = 16
	for offset := 0; offset < len(data); offset += bytesPerLine {
		end := offset + bytesPerLine
		if end > len(data) {
			end = len(data)
		}
		line := data[offset:end]

		checksum := byte(len(line)) + byte(offset>>8) + byte(offset)
		fmt.Fprintf(out, ":%02X%04X00", len(line), offset)
		for _, b := range line {
			fmt.Fprintf(out, "%02X", b)
			checksum += b
		}
		fmt.Fprintf(out, "%02X\n", byte(-int8(checksum)))
	}
	fmt.Fprintln(out, ":00000001FF")
}
