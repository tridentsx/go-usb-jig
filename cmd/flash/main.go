// flash loads an Intel HEX firmware image into an FX2/FX2LP's internal RAM
// over USB, using the vendor request 0xA0 convention Cypress's own sample
// firmware and every common loader tool (fxload, cycfx2prog) implement:
// halt the 8051 (CPUCS bit 0), write each hex record's bytes to its address
// via a vendor OUT control transfer, then release the 8051 (CPUCS bit 0
// cleared) so it starts running the new code.
//
// This is a RAM load, not an EEPROM write: it is not persistent, and it is
// fully reversible by unplugging the board, which either re-loads whatever
// firmware is in EEPROM (if any) or leaves it in its default state. Written
// here rather than depending on fxload/cycfx2prog because neither is
// packaged for this machine, and the protocol is only a handful of control
// transfers -- exactly what this project's own ControlTransfer path has
// already been verified against real hardware to do correctly.
package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	usb "github.com/kevmo314/go-usb"
)

// fwChunkSize matches libsigrok's ezusb.c FW_CHUNKSIZE exactly, for parity
// with the known-good loader this one is being cross-checked against.
const fwChunkSize = 4 * 1024

// parseRawBinary treats the whole file as one contiguous image starting at
// address 0, chunked for the control transfer -- the format libsigrok's
// ezusb_install_firmware uses for the .fw firmware images it bundles (as
// opposed to sdcc's per-record-addressed Intel HEX output).
func parseRawBinary(path string) ([]hexRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<16 {
		return nil, fmt.Errorf("firmware image is %d bytes, larger than the 64KiB a 16-bit address can reach", len(data))
	}

	var records []hexRecord
	for offset := 0; offset < len(data); offset += fwChunkSize {
		end := offset + fwChunkSize
		if end > len(data) {
			end = len(data)
		}
		records = append(records, hexRecord{address: uint16(offset), data: data[offset:end]})
	}
	return records, nil
}

const (
	cpucsAddress = 0xE600
	vendorLoad   = 0xA0

	// bmRequestType for a vendor, host-to-device, device-recipient control
	// transfer -- the direction and type bits any 0xA0 loader uses.
	bmRequestTypeOut = 0x40
)

type hexRecord struct {
	address uint16
	data    []byte
}

// parseIntelHex reads the subset of the Intel HEX format sdcc emits: type 00
// (data) and type 01 (end of file) records. Types 02-05 (extended
// segment/linear address, start address) are not handled, since sdcc's
// mcs51 output for a design this small does not need them -- addresses fit
// in 16 bits without an offset record.
func parseIntelHex(path string) ([]hexRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []hexRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		if line[0] != ':' {
			return nil, fmt.Errorf("malformed line, missing leading ':': %q", line)
		}
		raw, err := hex.DecodeString(line[1:])
		if err != nil {
			return nil, fmt.Errorf("decoding %q: %w", line, err)
		}
		if len(raw) < 5 {
			return nil, fmt.Errorf("line too short: %q", line)
		}

		byteCount := int(raw[0])
		address := uint16(raw[1])<<8 | uint16(raw[2])
		recordType := raw[3]
		if len(raw) != 5+byteCount {
			return nil, fmt.Errorf("byte count %d does not match line length: %q", byteCount, line)
		}

		var checksum byte
		for _, b := range raw[:len(raw)-1] {
			checksum += b
		}
		checksum = byte(-int8(checksum))
		if checksum != raw[len(raw)-1] {
			return nil, fmt.Errorf("checksum mismatch on %q: computed %#x, want %#x", line, checksum, raw[len(raw)-1])
		}

		switch recordType {
		case 0x00:
			records = append(records, hexRecord{address: address, data: raw[4 : 4+byteCount]})
		case 0x01:
			return records, nil
		default:
			return nil, fmt.Errorf("unsupported Intel HEX record type %#x on %q; this loader only handles data and EOF records", recordType, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, fmt.Errorf("no end-of-file record found")
}

func vendorWrite(handle *usb.DeviceHandle, address uint16, data []byte) error {
	_, err := handle.ControlTransfer(bmRequestTypeOut, vendorLoad, address, 0, data, 2*time.Second)
	return err
}

func main() {
	vendorID := flag.Uint("vid", 0x0925, "USB vendor ID of the board to flash")
	productID := flag.Uint("pid", 0x3881, "USB product ID of the board to flash")
	path := flag.String("hex", "firmware/firmware.ihx", "path to the firmware image (.ihx parsed as Intel HEX, anything else as a raw binary loaded from address 0)")
	flag.Parse()

	var records []hexRecord
	var err error
	if strings.EqualFold(filepath.Ext(*path), ".ihx") {
		records, err = parseIntelHex(*path)
	} else {
		records, err = parseRawBinary(*path)
	}
	if err != nil {
		log.Fatalf("parsing %s: %v", *path, err)
	}
	fmt.Printf("parsed %d data record(s) from %s\n", len(records), *path)

	devices, err := usb.DeviceList()
	if err != nil {
		log.Fatalf("DeviceList: %v", err)
	}
	var board *usb.Device
	for _, d := range devices {
		if d.Descriptor.VendorID == uint16(*vendorID) && d.Descriptor.ProductID == uint16(*productID) {
			board = d
			break
		}
	}
	if board == nil {
		log.Fatalf("no device %04x:%04x attached", *vendorID, *productID)
	}

	handle, err := board.Open()
	if err != nil {
		log.Fatalf("Open: %v", err)
	}
	defer handle.Close()
	fmt.Println("opened board, halting 8051...")

	if err := vendorWrite(handle, cpucsAddress, []byte{0x01}); err != nil {
		log.Fatalf("halting 8051 (CPUCS=1): %v", err)
	}

	for _, r := range records {
		if err := vendorWrite(handle, r.address, r.data); err != nil {
			log.Fatalf("writing %d byte(s) at %#04x: %v", len(r.data), r.address, err)
		}
	}
	fmt.Printf("wrote %d record(s)\n", len(records))

	if err := vendorWrite(handle, cpucsAddress, []byte{0x00}); err != nil {
		log.Fatalf("releasing 8051 (CPUCS=0): %v", err)
	}
	fmt.Println("released 8051; board should re-enumerate with the new firmware")
}
