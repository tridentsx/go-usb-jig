# fx2lib (vendored)

Source-only copy of `fx2lib`, as bundled inside the `sigrok-firmware-fx2lafw`
project (`fx2lib/` subdirectory there), used here to build `go-usb-jig`'s own
FX2 firmware instead of a from-scratch register file.

- Upstream: `sigrok-firmware-fx2lafw` (search for it — a real, official
  sigrok project repository; the `fx2lib` directory inside it is this copy).
- License: GPLv2+ (`COPYING`) with some LGPL-licensed files (`COPYING.LESSER`)
  — see each file's own header for which applies.
- No build artifacts are committed here; `../Makefile` builds `fx2.lib` and
  `interrupts/ints.lib` from this source.

`../../hw/go-usb-jig/dscr.a51` and `common.inc`'s macros it uses are adapted
from `sigrok-firmware-fx2lafw`'s own descriptor pattern (`dscr.inc`/
`dscr.a51`), not from `fx2lib` itself; see that file's own header comment.
