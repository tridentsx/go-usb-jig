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

## Endpoint map

One interface, one alternate setting (deliberately simpler than the multi
alt-setting descriptor set FX2 boards often ship with — that exists for
bandwidth negotiation, which a test jig doesn't need):

| Endpoint | Type | Purpose |
|---|---|---|
| EP1 IN | Interrupt | Free-running heartbeat byte |
| EP2 OUT | Bulk | Loopback source |
| EP6 IN | Isochronous | Free-running counter pattern |
| EP8 IN | Bulk | Echoes whatever EP2 OUT last received |
| EP0 | Control (vendor) | Stall-an-endpoint, read/write internal RAM |

## Firmware status

**Draft, not yet verified against real hardware.** It compiles cleanly with
SDCC (`cd firmware && make`), but the register-level constants in
`firmware/fx2regs.h` and several mechanisms in `firmware/main.c` (marked
`VERIFY` in comments) were written from memory rather than checked against
the Cypress EZ-USB FX2LP Technical Reference Manual, unlike the macOS IOKit
work in the main go-usb repo, which was checked against the actual SDK
headers line by line. Getting a register address wrong here doesn't fail to
compile — it silently misconfigures the chip. Cross-check every `VERIFY`
comment against the TRM before flashing, or consider rebuilding on top of
`fx2lib` (a maintained, tested open-source FX2 register/USB library —
search for it rather than trusting a pasted link), which would remove most
of that risk at the cost of an external dependency.

Building firmware.ihx does not flash it. A separate loader tool
(`fxload`/`cycfx2prog`, not included here) writes it into the FX2's RAM over
USB.

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
