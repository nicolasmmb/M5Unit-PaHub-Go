# Benchmarks — M5Unit-PaHub-Go

## Method

- Host: Apple Silicon (darwin/arm64), Go 1.26.3, `-count=3 -benchtime=200ms`.
- Bus fake (`silentBus`): answers like a live hub and keeps no history.
- No allocation in the measured path.

## Results

| Benchmark | Scenario | ns/op | allocs/op | bus calls/op |
|---|---|---:|---:|---:|
| `Tx` | read mask, set channel, device op, restore | ~18,2 | 0 | 4 |
| `Select` | single write | ~13,2 | 0 | 1 |
| `Enable` | read + write | ~12,7 | 0 | 2 |
| `Mask` | single read | ~12,7 | 0 | 1 |

The host numbers only show the driver overhead. On the wire the I2C bus
dominates: at 400 kHz a 2-byte transfer is about 50–60 µs, so one `Tx` costs
about 200–250 µs of bus time, and `Select` about 55 µs.

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

## Measurements still missing

| Item | Why it matters |
|---|---|
| Real `Select` latency on hardware | The host number ignores the I2C transfer |
| Mask read-back on a real PCA9548A | Confirms the presence check |
| Behaviour with a device on two channels with the same address | The main use case of the unit |
| Nested hubs (two devices, two addresses) | Confirms the "restore" decision |
