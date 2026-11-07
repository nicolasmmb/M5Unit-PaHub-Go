# M5Unit-PaHub-Go

[![test](https://github.com/nicolasmmb/M5Unit-PaHub-Go/actions/workflows/test.yml/badge.svg)](https://github.com/nicolasmmb/M5Unit-PaHub-Go/actions/workflows/test.yml)

Pure-Go driver for the **M5Stack Unit PaHub v2.1** (SKU **U040-B-V21**, chip
**PCA9548A**) and for the older **Unit PaHub** (SKU U040, TCA9548A). Both chips
share the same control register, so one driver serves both. No C, C++, CGo or
Arduino.

The unit turns one Grove I2C port into six, so devices with the same address can
coexist: only the selected channels reach the main bus.

The package knows nothing about boards. It depends only on the two-argument I2C
interface, so `machine.I2C` satisfies it on any chip and a fake bus makes the
driver testable on the host.

## Features

- `Select`, `SelectAll` (same semantics as `portselect`/`portselectall` in the
  official Arduino example), `Enable`, `Disable`, `Mask`, `Close`.
- `Tx`: one call that selects the channel, runs the I2C transaction and
  **restores the previous mask**.
- `Scan(channel)`: list the addresses that answer on one channel.
- Presence check without an ID register, using the read-back of the control
  register.
- Every operation is serialized: the channel mask is global bus state.
- Zero heap allocation per operation. `Device` is 32 bytes.
- Host tests with a PCA9548A fake, allocation and footprint contracts, and an
  agnosticism gate in CI (compiles for RP2040 and nRF52840).

## Hardware

| Parameter | v2.1 | v1 |
|---|---|---|
| SKU | U040-B-V21 | U040 |
| Chip | PCA9548A | TCA9548A |
| Address | 0x70–0x77 by DIP switch | 0x70–0x77 by solder pads |
| Channels used | 6 of 8 | 6 of 8 |
| Clock | up to 400 kHz | up to 400 kHz |
| Connector | Grove HY2.0-4P on PORT A (GND, 5 V, SDA, SCL) | same |

Control register: one byte, bit N enables channel N. After power-on every
channel is disabled (mask `0x00`). Writing `0x00` disables all channels.

The hub has no buffer: it connects the SDA/SCL pair of the selected channels to
the main bus. Several bits can be set at the same time, which is what makes
nested hubs work.

## Install

```bash
go get github.com/nicolasmmb/M5Unit-PaHub-Go@v0.1.0
```

## Quick start

```go
i2c := machine.I2C1
_ = i2c.Configure(machine.I2CConfig{
    SDA:       machine.GPIO32,
    SCL:       machine.GPIO33,
    Frequency: 100 * machine.KHz,
})

hub := pahub.New(i2c)
if err := hub.Init(); err != nil {
    println("pahub:", err.Error())
    return
}

// Scan what is behind each channel.
for ch := uint8(0); ch < pahub.NumChannels; ch++ {
    found, err := hub.Scan(ch)
    println("ch", ch, "devices", len(found), "err", err)
}

// One transaction on channel 2: a ToF90 at 0x29, for example.
var id [1]byte
if err := hub.Tx(2, 0x29, []byte{0xC0}, id[:]); err != nil {
    println("tof:", err.Error())
}
println("model id", id[0])
```

## API

| Function | Description |
|---|---|
| `New(bus I2C) *Device` | Default address 0x70 |
| `NewAt(bus, addr) (*Device, error)` | 0x70–0x7f, checks presence |
| `Init() error` | Disables all channels and confirms the hub answers |
| `Address() uint16` | Configured address |
| `Select(ch) error` | Enables exactly one channel (like `portselect`) |
| `SelectAll(mask) error` | Writes the mask, masked to 6 bits (like `portselectall`) |
| `Enable(ch) error` | OR with the current mask (like `select_channel`) |
| `Disable(ch) error` | AND with `^(1<<ch)` (like `release_channel`) |
| `Mask() (uint8, error)` | Reads the control register (presence probe) |
| `Close() error` | Mask 0 |
| `Tx(ch, addr, w, r) error` | Select, transact, restore |
| `TxContext(ctx, ch, addr, w, r) error` | Same with a cancellation check |
| `Scan(ch) ([]uint8, error)` | Addresses that answer, hub excluded |

Errors: `ErrNotConnected`, `ErrBadAddress`, `ErrBadChannel`. Compare with
`errors.Is`.

## Design decisions

1. **`Tx` restores the previous mask.** The UIFlow driver clears the bit of the
   channel it used. Restoring is safer: it keeps channels that other code
   enabled, which is what nested hubs need. Tested in `TestTxRestoresMask`.
2. **Everything is serialized by a mutex.** Two goroutines on different channels
   would otherwise interleave their mask writes and talk to the wrong device.
3. **No `sleep`.** The UIFlow `writeto` waits 100 ms after every write; the
   datasheet does not require it, so it is not copied.
4. **Presence without an ID register.** The PCA9548A has no identification
   register, so `Init` and `NewAt` write and read back the control register.
5. **`Scan` allocates.** It returns a slice and is a diagnostic, not a control
   loop call.

## Cost on the wire

Each `Tx` performs four bus transactions:

```
read mask  ->  write mask (channel on)  ->  device transaction  ->  write mask (restore)
```

At 400 kHz each of these is roughly 50–60 µs, so a `Tx` costs about 200–250 µs
of bus time. `Select` is one write. If that matters, cache the mask: see the
gaps section.

## Benchmarks

Host, fake bus, so the numbers show only the driver cost (see
[BENCHMARKS.md](BENCHMARKS.md)):

| Operation | ns/op | allocations |
|---|---:|---:|
| `Tx` (4 bus calls) | ~18 | 0 |
| `Select` | ~13 | 0 |
| `Enable` | ~13 | 0 |
| `Mask` | ~13 | 0 |

RAM: `Device` 32 bytes. Flash: `examples/scan` 20.1 KB, `examples/validate`
19.3 KB on `m5stick-c`.

## Hardware results

Validated on an M5StickC Plus2 with a real Unit PaHub v2.1, one test per flash:

| Test | Result |
|---|---|
| 1 presence | PASS, `mask after Init = 0` |
| 2 select 0..5 | PASS, masks `1, 2, 4, 8, 16, 32` read back |
| 3 scan all channels | PASS, six channels scanned, all empty (no devices attached) |
| 4 restore mask after `Tx` | PASS, mask returns to the previous value |
| 5 two bits enabled (nesting) | PASS, `ch0 + ch3 = 0x09` |

### Finding: the mask survives an MCU reset

The unit stays powered by the Grove 5 V rail while the MCU resets, so the
PCA9548A keeps the last mask. A firmware that assumes `0x00` after reset will
talk to the wrong channel.

**Always call `Init()` (or `Close()`) at boot.** The first run of test 4 showed
`mask = 33` instead of `1` for exactly this reason: the mask was left by the
previous test.

## Validation

`examples/validate` runs **one test per flash**, in a loop. Select the test in
`examples/validate/config.go`:

| `test` | Behaviour |
|---:|---|
| 1 | presence: mask is 0x00 after `Init` |
| 2 | select each channel and read the mask back |
| 3 | scan all six channels and print the addresses |
| 4 | restore: enable ch0, transact on ch2, mask must come back to 0x01 |
| 5 | nesting: ch0 + ch3 enabled, mask 0x09 |

```bash
make validate PORT=/dev/cu.usbserial-XXXX
make scan PORT=/dev/cu.usbserial-XXXX
```

## Known gaps

| Gap | Effect | Next step |
|---|---|---|
| Mask is read on every operation | `Tx` costs 4 bus transactions instead of 3 | Cache the mask in `Device`, invalidate on `Mask`/`Init`; keep the read as an option |
| No `/INT` support | The unit does not expose the interrupt pin | Nothing to do on this unit |
| Nested hubs are manual | Two `Device` values and two address writes | Add a `Route` helper if it is used often |
| ~~Not validated in hardware~~ | Validated on a real PaHub v2.1: 5/5 tests | Done |
| `Scan` allocates | Not usable in a control loop | Provide `ScanInto(dst []uint8)` if needed |

## References

- PaHub v2.1: https://docs.m5stack.com/en/unit/Unit_PaHub_v2.1
- PaHub v1 (TCA9548A): https://docs.m5stack.com/en/unit/Unit_PaHub
- Official example: `M5-ProductExampleCodes/Unit/PaHUB/Arduino/PaHUB/PaHUB.ino`
- Official MicroPython: `uiflow-micropython/m5stack/libs/unit/pahub.py`
- Datasheet: PCA9548A (NXP), TCA9548A (TI)

## License

MIT — see [LICENSE](LICENSE).
