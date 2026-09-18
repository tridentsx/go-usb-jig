# go-usb-jig

A hardware test jig for [go-usb](https://github.com/kevmo314/go-usb): FX2
firmware plus hardware-gated Go tests, exercising bulk, interrupt,
isochronous and vendor control transfers against real USB hardware that
no operating system's built-in class driver will claim.

## Why this exists

Standard-class USB devices — UVC webcams, USB Audio Class devices — get
claimed exclusively by the OS's own driver stack the moment they're plugged
in (on macOS, permanently and unavoidably from user space; confirmed even
after killing the relevant system extensions and watching them respawn
instantly). That makes them useless for testing a raw-USB library's
isochronous and interface-claiming code. A vendor-specific-class device
(`bDeviceClass 0xFF`) has no such competitor, so this jig's firmware
deliberately declares itself vendor-specific throughout.

The board used during development is a Cypress FX2/FX2LP dev board
(`0925:3881`), the common default VID:PID for that chip's stock loader
firmware — cheap and widely available.

**Update (2026-09-17):** the actual board on hand turned out to be a cheap
FX2-based USB logic analyzer clone (24MHz, 8-channel, sealed plastic
enclosure, no buttons or jumpers — instantly recognizable by that
description), running real application firmware from an onboard EEPROM
rather than a bare bootloader. A first from-scratch firmware attempt
(register values transcribed from memory, no verified source) never got
past enumeration — the standard `0xA0` RAM-download command was accepted at
the USB protocol level but the new code never actually took effect.

`cmd/flash` was cross-checked against known-good firmware to rule out the
loader itself: loading real `fx2lafw` binaries (bundled with `libsigrok`,
one via `cmd/flash`'s raw-binary path, one converted to Intel HEX via
`cmd/bin2hex` and loaded via its `.ihx` path) both correctly changed the
device's VID:PID to match whichever image was loaded. **The loader was
proven correct**, isolating the bug to the firmware itself.

The fix: rather than keep debugging a from-scratch register file with no
verified source, the real `sigrok-firmware-fx2lafw` firmware source and its
`fx2lib` dependency were fetched (via `git clone`, not a paraphrased
summary) and built from scratch with this project's own SDCC to confirm the
whole toolchain end-to-end — that fresh build, flashed with `cmd/flash`,
correctly re-enumerated as real `fx2lafw`. `fx2lib` (source only, no build
artifacts) is now vendored into `firmware/fx2lib/`, and this project's own
firmware (`firmware/hw/go-usb-jig/`) is built on top of it instead of a
hand-rolled register file. **That firmware now boots and enumerates
correctly on real hardware, and every transfer type go-usb supports on
this platform works end to end**: bulk, interrupt, isochronous, control
transfers with a real multi-packet data stage, and endpoint stall/
clear-halt all pass on a fresh flash.

Two more bugs turned up chasing what first looked like firmware problems,
neither of which was actually in the firmware — see "Firmware status"
below for what they really were.

## Endpoint map

One interface, one alternate setting (deliberately simpler than the multi
alt-setting descriptor set FX2 boards often ship with — that exists for
bandwidth negotiation, which a test jig doesn't need):

| Endpoint | Type | Purpose |
|---|---|---|
| EP1 IN | Interrupt | Free-running heartbeat byte |
| EP2 OUT | Bulk | Loopback source |
| EP6 IN | Bulk | Echoes whatever EP2 OUT last received |
| EP8 IN | Isochronous | Free-running counter byte, one packet/microframe |
| EP0 | Control | Standard requests, plus a custom vendor RAM read/write; endpoint stall/clear-halt via the standard SET_FEATURE/CLEAR_FEATURE(ENDPOINT_HALT) requests |

`EP4` is unused.

## Firmware status

**Boots and enumerates correctly on real hardware**, built on `fx2lib`
(vendored in `firmware/fx2lib/`, source only — see its own `README`/
`COPYING`) instead of a from-scratch register file. `cd firmware && make`
builds `firmware.ihx` reproducibly from a clean checkout (verified: `make
clean && make` was run and produced an identical build before this was
written). Building it does not flash it — that's `cmd/flash`, a separate
step over USB.

The firmware had one real bug of its own (below); everything else that
looked like a firmware bug at first turned out to be elsewhere:

- **Bulk transfers to EP2 OUT eventually timing out for good, from both
  macOS (IOKit) and Windows (WinUSB) against the identical firmware.**
  This one really was in the firmware: `main()`'s copy loop calls
  `OUTPKTEND` to release each EP2 OUT buffer it's consumed, but
  `OUTPKTEND` has no effect at all unless `REVCTL.0` (`ENH_PKT`) is set —
  confirmed against the real TRM's own register reference for `OUTPKTEND`
  (Section 15), not the more casual usage examples elsewhere in the same
  manual that don't mention this gate. This firmware never set `REVCTL`,
  so every `OUTPKTEND` call was a silent no-op: the very first OUT packet
  committed correctly (a special case that doesn't need `OUTPKTEND`), but
  every buffer after that never actually got released, so the quad
  buffer filled permanently after 3-4 packets and stayed that way —
  across every subsequent software reflash, since `cmd/flash`'s `0xA0`
  command halts and reloads the 8051 CPU only, never touching the USB
  SIE/FIFO hardware that was actually stuck.

  Found with a custom vendor request (`VR_READ_REGS`, exposed as
  `TestDumpRegisters`) added specifically to read live `EP2CS`/`EP6CS`/
  `EP2468STAT` and two firmware-side counters over USB, which showed
  `EP2CS` reporting `NPAK=4`/`FULL` unchanged across a failed write
  attempt and across repeated attempts to clear it — ruling out a go-usb
  bug on either platform (confirmed independently on macOS: a raw,
  synchronous `WritePipe` with no timeout logic at all still never got a
  response), and ruling out a hub or cable issue (the same symptom
  persisted through a `AbortPipe` call, which should have cancelled any
  host-side outstanding request). Fixed with `REVCTL = 0x03;` in
  `setup_endpoints`, matching every real TRM example that calls
  `OUTPKTEND`.
- **Bulk loopback occasionally returning a fixed repeating pattern
  instead of the echoed data.** Not a firmware bug: running the test suite
  repeatedly against an already-running device (no reflash between runs)
  left stale packets in `EP6 IN`'s quad buffer from a previous run. Fixed
  in `jig_test.go`'s `openJig` by calling `SetInterfaceAltSetting(0, 0)`
  after claiming, which triggers the firmware's `handle_set_interface` and
  resets both endpoints' FIFOs on every test run, not just after a flash.
- **The REVCTL fix above needed two follow-ups to be usable without a
  physical power cycle or repeat-write corruption**, both found verifying
  it on real Windows/WinUSB hardware:
  - `RESETFIFO` (already called on every boot) only resets FIFO pointers,
    not the SIE's count of already-accepted, unconsumed packets — `EP2CS`
    still read back `NPAK=4`/`FULL` immediately after a software-only
    reflash with the REVCTL fix applied. `setup_endpoints` now
    unconditionally drains EP2 OUT's quad buffer via `OUTPKTEND` on every
    boot (the same mechanism `VR_RESET_BULK` already used on demand), so a
    plain `go run ./cmd/flash` is enough on its own; a physical power
    cycle is no longer required.
  - The main loop's own `OUTPKTEND` call had no settling delay before the
    next iteration re-read `EP2468STAT`, unlike `VR_RESET_BULK`'s drain
    loop, which already waited four `SYNCDELAY`s per call. Without it, a
    single host write could be seen and re-committed to `EP6` up to three
    times (`ep2_seen_count`/`ep6_committed_count` read 3 for one write),
    producing mismatched/garbage echoed data. Fixed by adding the same
    four-`SYNCDELAY` wait there.
- **The interrupt endpoint (`EP1 IN`) returning `kIOReturnBadArgument`.**
  A real bug, but in [go-usb](https://github.com/kevmo314/go-usb), not this
  firmware: its darwin backend's `InterruptTransfer` delegated to
  `BulkTransfer`, which defaults a zero timeout to 5000ms and therefore
  always called IOKit's `ReadPipeTO`/`WritePipeTO`. Confirmed against this
  board's real interrupt endpoint that those timeout variants fail with
  `kIOReturnBadArgument` specifically for interrupt-type pipes, while the
  plain (non-timeout) `ReadPipe`/`WritePipe` on the exact same pipe
  succeed. Fixed upstream (see go-usb's `transfer_darwin.go`); a second,
  unrelated bug in this repo's own `TestInterruptHeartbeat` (comparing two
  slices that aliased the same backing array, so it could never have
  detected a real advance) was fixed alongside it.

`EP8`'s isochronous counter, the vendor RAM read/write command, and
endpoint stall/clear-halt were added after the bulk/interrupt baseline
above was confirmed solid, and all three work on real hardware, though the
isochronous endpoint is worth a specific note: this firmware fills it from
a plain polling loop with no SOF (start-of-frame) synchronization, so
individual packets in a burst legitimately come back with a non-success
`IOReturn` (observed: `kIOReturnOverrun` early in a burst, then
`kIOReturnUnderrun`) even though real data does get through overall. This
matches the USB 2.0 spec's own isochronous guarantees (no retries, no
guarantee every microframe is serviced — section 5.6.4), not a bug in
go-usb or this firmware; `TestIsochronousCounter` only checks that some
data arrived across the whole burst, not that every packet is clean. A
firmware that services EP8 from the SOF interrupt instead of the main
polling loop would likely clean this up, but wasn't needed to get real,
verifiable isochronous I/O working end to end.

Unlike the from-scratch attempt's `fx2regs.h`, nothing here needs a `VERIFY
against the TRM` disclaimer — every register/macro used comes from
`fx2lib`'s real, shipped source, cross-checked against
`sigrok-firmware-fx2lafw`'s own `fx2lafw.c` (also fetched via `git clone`,
not paraphrased) for usage patterns.

## Running the tests

```
go test -tags jig ./...
```

The `jig` build tag keeps these entirely out of `go test ./...` without it —
they need the board attached and running this repo's firmware, not the
stock firmware it ships with. Each test skips (not fails) if the board
isn't found at all, but will fail with a clear "no pipe for endpoint"
error if it's attached but still running different firmware. Flash this
repo's firmware first with `go run ./cmd/flash firmware/firmware.ihx`
(build it with `cd firmware && make` if `firmware.ihx` isn't there yet).

## Windows setup

The board and its firmware are entirely platform-independent — the same
`firmware.ihx` that's already flashed and verified against macOS works
unchanged once the board is physically moved to a Windows machine.
Two things are specific to Windows, though:

1. **Bind WinUSB to the board with [Zadig](https://zadig.akeo.ie/).**
   The board enumerates as a vendor-specific class device (`bDeviceClass
   0xFF`, deliberately — see "Why this exists" above), so Windows has no
   built-in class driver for it and `go-usb`'s Windows backend cannot open
   it until some driver is bound. Run Zadig, select the device
   (`0925:3881`), and install the WinUSB driver for it. This should only be
   needed once: the firmware keeps the same VID:PID across reflashes, and
   WinUSB's driver association is keyed on that, not on the exact
   descriptor content. If Device Manager ever shows the board under a
   different driver after a reflash, redo this step.
2. **Building the firmware needs SDCC on Windows too, if you rebuild it.**
   You almost certainly don't need to: `firmware.ihx` is compiled 8051
   machine code with no dependency on the host platform that built it, so
   copying the already-built file from wherever it was last built (and
   flashing it with `go run ./cmd/flash firmware/firmware.ihx`, same as
   everywhere else) is enough. Only install SDCC and run `cd firmware &&
   make` on Windows if you're actually changing the firmware source itself.

`TestIsochronousCounter` now has a real implementation on Linux and Windows
too (`isochronous_other_test.go`, via go-usb's portable
`NewIsochronousTransfer`/`Submit`/`Wait`), not just macOS
(`isochronous_darwin_test.go`). Verified against this board's real hardware
on Windows 11 over WinUSB.

## go.mod

Depends on `github.com/kevmo314/go-usb`, replaced to point at a commit on
[tridentsx/go-usb](https://github.com/tridentsx/go-usb)'s `windows/isochronous`
branch (stacked on `windows/root-hubs`, not yet merged upstream) for real
Windows isochronous and async-transfer support. Update the `replace`
directive (or remove it) once that lands on `main`.
