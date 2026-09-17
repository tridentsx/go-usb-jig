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
correctly on real hardware** — `ClaimInterface` succeeds, `BulkTransfer`
OUT succeeds, and a `BulkTransfer` IN on the loopback endpoint returns real
data end-to-end (mechanism fully verified; the loopback's *content* still
has a bug — it echoes a fixed pattern instead of what was sent, and the
interrupt endpoint returns `kIOReturnBadArgument` — both real, tractable
firmware-logic bugs to chase next, a much better place to be than "nothing
boots").

## Endpoint map

One interface, one alternate setting (deliberately simpler than the multi
alt-setting descriptor set FX2 boards often ship with — that exists for
bandwidth negotiation, which a test jig doesn't need):

| Endpoint | Type | Purpose |
|---|---|---|
| EP1 IN | Interrupt | Free-running heartbeat byte |
| EP2 OUT | Bulk | Loopback source |
| EP6 IN | Bulk | Echoes whatever EP2 OUT last received |
| EP0 | Control | Standard requests only for now (see below) |

Isochronous and vendor RAM/stall commands were in the original (broken)
design and are not yet back in the `fx2lib`-based rebuild; they're the
natural next addition once the bulk/interrupt baseline above is fully
debugged. `EP8` isn't used in this version.

## Firmware status

**Boots and enumerates correctly on real hardware**, built on `fx2lib`
(vendored in `firmware/fx2lib/`, source only — see its own `README`/
`COPYING`) instead of a from-scratch register file. `cd firmware && make`
builds `firmware.ihx` reproducibly from a clean checkout (verified: `make
clean && make` was run and produced an identical build before this was
written). Building it does not flash it — that's `cmd/flash`, a separate
step over USB.

Known real bugs, both firmware-logic issues rather than toolchain/protocol
problems:
- The bulk loopback (`EP2 OUT` → `EP6 IN`) mechanism works, but the
  content is wrong: it returns a fixed repeating pattern instead of
  echoing what was sent.
- The interrupt endpoint (`EP1 IN`) returns `kIOReturnBadArgument`.

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
error if it's attached but still running different firmware — which is
exactly what happens today, since the custom firmware hasn't been flashed
yet.

## go.mod

Depends on `github.com/kevmo314/go-usb`, replaced to point at
[tridentsx/go-usb](https://github.com/tridentsx/go-usb)'s
`darwin/isochronous` branch until PRs #18–#21 merge upstream. Update the
`replace` directive (or remove it) once they do.
