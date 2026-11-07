# Benchmarks — M5Unit-PaHub-Go

## Method

- Host: Apple Silicon (darwin/arm64), Go 1.26.3, `-count=3 -benchtime=200ms`.
- Bus fake (`silentBus`): answers like a live hub and keeps no history.
- No allocation in the measured path.

## Results

| Benchmark | Scenario | ns/op | allocs/op | bus calls/op |
|---|---|---:|---:|---:|
| `Tx` (warm cache) | set channel, device op, restore | ~18,2 | 0 | 3 |
| `Select` | single write | ~13,2 | 0 | 1 |
| `Enable` | read + write | ~12,7 | 0 | 2 |
| `Mask` | single read | ~12,7 | 0 | 1 |

The host numbers only show the driver overhead. On the wire the I2C bus
dominates: at 400 kHz a 2-byte transfer is about 50–60 µs, so a warm `Tx` costs
about 150–180 µs of bus time, and `Select` about 55 µs. The first call after
`New` or after `Invalidate()` adds one read: ~200–250 µs.

## Footprint

| Type | Size | Budget in the test |
|---|---:|---:|
| `Device` | 32 B | ≤ 48 B |

## Flash (TinyGo, `m5stick-c`)

| Target | code | data | bss | flash | ram |
|---|---:|---:|---:|---:|---:|
| `examples/scan` | 19 976 | 124 | 4 628 | 20 100 B | 4 752 B |
| `examples/validate` | 19 156 | 124 | 4 628 | 19 280 B | 4 752 B |

## Contracts checked

```bash
make alloc      # zero allocation and footprint
make agnostic   # compiles the constructor for pico and arduino-nano33
```

- `Select`, `SelectAll`, `Enable`, `Disable`, `Mask`, `Close`, `Tx`,
  `TxContext`: **0 allocs/op**.
- `New` makes one allocation (the `Device`).
- `Scan` allocates the result slice on purpose.

## Hardware measurements (M5StickC Plus2 + PaHub v2.1)

| Test | Result | Note |
|---|---|---|
| Presence (`Init` + read-back) | PASS | mask 0 after write |
| `Select` 0..5 | PASS | read-back `1,2,4,8,16,32` |
| `Scan` on six empty channels | PASS | 0 devices, mask restored to 0 |
| Mask restore after `Tx` | PASS | mask returns to the previous value |
| Two bits enabled | PASS | `0x09` for ch0 + ch3 |

Timing not measured on the target: the I2C transfer dominates and needs a scope
or a bench supply to be measured properly. The mask read-back is the timing-free
proof that the writes land.

## Measurements still missing

| Item | Why it matters |
|---|---|
| Real `Select` latency on hardware | The host number ignores the I2C transfer |
| Behaviour with a device on two channels with the same address | The main use case of the unit — needs one device plugged into two ports |
| Real `Tx` timing at 100/400 kHz | Needs a scope or a bench measurement |
| Nested hubs (two devices, two addresses) | Confirms the "restore" decision |
