package pahub

import (
	"context"
	"errors"
	"sync"
	"testing"
)

var errNack = errors.New("fake: nack")

// fakeDev is one device behind a channel. It has one byte of state.
type fakeDev struct {
	addr  uint16
	ch    uint8
	value byte
	wrote int
}

// fakeBus emulates a PCA9548A at hubAddr and any number of devices behind it.
// A device answers only when its channel bit is set in the mask.
func fakeBus(devs ...*fakeDev) *fakeI2C {
	return &fakeI2C{hubAddr: DefaultAddress, devs: devs}
}

func fakeBusAt(addr uint16, devs ...*fakeDev) *fakeI2C {
	return &fakeI2C{hubAddr: addr, devs: devs}
}

type fakeI2C struct {
	hubAddr uint16
	mask    uint8
	devs    []*fakeDev
	dead    bool
	maskLog []uint8
	txCount int
}

func (b *fakeI2C) Tx(addr uint16, w, r []byte) error {
	b.txCount++
	if addr == b.hubAddr {
		if b.dead {
			return errNack
		}
		if len(w) > 0 {
			b.mask = w[0] & 0x3f
			b.maskLog = append(b.maskLog, b.mask)
		}
		if len(r) > 0 {
			r[0] = b.mask
		}
		return nil
	}
	for _, d := range b.devs {
		if d.addr != addr || b.mask&(1<<d.ch) == 0 {
			continue
		}
		if len(w) > 0 {
			d.value = w[len(w)-1]
			d.wrote++
		}
		if len(r) > 0 {
			r[0] = d.value
		}
		return nil
	}
	return errNack
}

// NewAt2 skips the presence check, useful for fakes with another address.
func NewAt2(bus I2C, addr uint16) *Device { return &Device{bus: bus, addr: addr} }

func newDev(t *testing.T) (*Device, *fakeI2C) {
	t.Helper()
	bus := fakeBus()
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return d, bus
}

func TestPresence(t *testing.T) {
	bus := fakeBus()
	if err := New(bus).Init(); err != nil {
		t.Fatalf("Init on live hub: %v", err)
	}
	bus.dead = true
	if err := New(bus).Init(); err != ErrNotConnected {
		t.Fatalf("dead hub: %v", err)
	}
	if _, err := NewAt(bus, DefaultAddress); err != ErrNotConnected {
		t.Fatalf("NewAt dead hub: %v", err)
	}
}

func TestBadAddress(t *testing.T) {
	bus := fakeBus()
	if _, err := NewAt(bus, 0x50); err != ErrBadAddress {
		t.Fatalf("low address: %v", err)
	}
	if _, err := NewAt(bus, 0x80); err != ErrBadAddress {
		t.Fatalf("high address: %v", err)
	}
	upper := fakeBusAt(0x77)
	d, err := NewAt(upper, 0x77)
	if err != nil {
		t.Fatalf("0x77: %v", err)
	}
	if d.Address() != 0x77 {
		t.Fatalf("address = %#02x", d.Address())
	}
}

func TestInitDisablesAll(t *testing.T) {
	bus := fakeBus()
	d := New(bus)
	_ = d.SelectAll(0x3f)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	if bus.mask != 0 {
		t.Fatalf("mask after Init = %#02x, want 0", bus.mask)
	}
	if got, err := d.Mask(); err != nil || got != 0 {
		t.Fatalf("Mask = %#02x, %v", got, err)
	}
}

func TestSelect(t *testing.T) {
	d, bus := newDev(t)
	for ch := uint8(0); ch < NumChannels; ch++ {
		if err := d.Select(ch); err != nil {
			t.Fatal(err)
		}
		if want := uint8(1) << ch; bus.mask != want {
			t.Fatalf("Select(%d) = %#02x, want %#02x", ch, bus.mask, want)
		}
	}
	if err := d.Select(NumChannels); err != ErrBadChannel {
		t.Fatalf("Select(6) = %v", err)
	}
}

func TestSelectAllMasksToSixBits(t *testing.T) {
	d, bus := newDev(t)
	if err := d.SelectAll(0xff); err != nil {
		t.Fatal(err)
	}
	if bus.mask != 0x3f {
		t.Fatalf("mask = %#02x, want 0x3f", bus.mask)
	}
	if got, _ := d.Mask(); got != 0x3f {
		t.Fatalf("Mask = %#02x", got)
	}
}

func TestEnableDisable(t *testing.T) {
	d, bus := newDev(t)
	if err := d.Enable(1); err != nil {
		t.Fatal(err)
	}
	if err := d.Enable(4); err != nil {
		t.Fatal(err)
	}
	if bus.mask != 0x12 {
		t.Fatalf("mask = %#02x, want 0x12", bus.mask)
	}
	if err := d.Disable(1); err != nil {
		t.Fatal(err)
	}
	if bus.mask != 0x10 {
		t.Fatalf("mask = %#02x, want 0x10", bus.mask)
	}
	if err := d.Disable(NumChannels); err != ErrBadChannel {
		t.Fatalf("Disable(6) = %v", err)
	}
	if err := d.Enable(NumChannels); err != ErrBadChannel {
		t.Fatalf("Enable(6) = %v", err)
	}
}

func TestClose(t *testing.T) {
	d, bus := newDev(t)
	_ = d.SelectAll(0x3f)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if bus.mask != 0 {
		t.Fatalf("mask after Close = %#02x", bus.mask)
	}
}

// TestTxRestoresMask checks the design decision: the mask of other channels is
// restored after a transaction, so nested hubs keep working.
func TestTxRestoresMask(t *testing.T) {
	dev := &fakeDev{addr: 0x20, ch: 2, value: 0xaa}
	bus := fakeBus(dev)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	if err := d.Enable(0); err != nil {
		t.Fatal(err)
	}
	var rx [1]byte
	if err := d.Tx(2, 0x20, nil, rx[:]); err != nil {
		t.Fatal(err)
	}
	if rx[0] != 0xaa {
		t.Fatalf("read %#02x, want 0xaa", rx[0])
	}
	if bus.mask != 0x01 {
		t.Fatalf("mask after Tx = %#02x, want 0x01 (channel 0 restored)", bus.mask)
	}

	// Channel with no device: the transaction fails but the mask is restored.
	if err := d.Tx(3, 0x20, nil, rx[:]); err == nil {
		t.Fatal("Tx on empty channel must fail")
	}
	if bus.mask != 0x01 {
		t.Fatalf("mask after failed Tx = %#02x, want 0x01", bus.mask)
	}
}

// TestSameAddressTwoChannels is the reason the hub exists.
func TestSameAddressTwoChannels(t *testing.T) {
	a := &fakeDev{addr: 0x20, ch: 0, value: 0x11}
	b := &fakeDev{addr: 0x20, ch: 1, value: 0x22}
	bus := fakeBus(a, b)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	var rx [1]byte
	if err := d.Tx(0, 0x20, nil, rx[:]); err != nil {
		t.Fatal(err)
	}
	if rx[0] != 0x11 {
		t.Fatalf("channel 0 = %#02x", rx[0])
	}
	if err := d.Tx(1, 0x20, nil, rx[:]); err != nil {
		t.Fatal(err)
	}
	if rx[0] != 0x22 {
		t.Fatalf("channel 1 = %#02x", rx[0])
	}
}

func TestWriteThroughHub(t *testing.T) {
	dev := &fakeDev{addr: 0x38, ch: 5}
	bus := fakeBus(dev)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	if err := d.Tx(5, 0x38, []byte{0x01, 0x7f}, nil); err != nil {
		t.Fatal(err)
	}
	if dev.value != 0x7f || dev.wrote != 1 {
		t.Fatalf("device value = %#02x writes = %d", dev.value, dev.wrote)
	}
}

func TestScan(t *testing.T) {
	a := &fakeDev{addr: 0x20, ch: 0}
	b := &fakeDev{addr: 0x50, ch: 1}
	c := &fakeDev{addr: 0x20, ch: 1}
	bus := fakeBus(a, b, c)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}

	ch0, err := d.Scan(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch0) != 1 || ch0[0] != 0x20 {
		t.Fatalf("scan(0) = %v", ch0)
	}

	ch1, err := d.Scan(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch1) != 2 || ch1[0] != 0x20 || ch1[1] != 0x50 {
		t.Fatalf("scan(1) = %v", ch1)
	}

	empty, err := d.Scan(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("scan(4) = %v", empty)
	}
	if bus.mask != 0 {
		t.Fatalf("mask after Scan = %#02x, want 0", bus.mask)
	}
	if _, err := d.Scan(NumChannels); err != ErrBadChannel {
		t.Fatalf("scan(6) = %v", err)
	}
}

func TestTxContext(t *testing.T) {
	dev := &fakeDev{addr: 0x20, ch: 0}
	bus := fakeBus(dev)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	var rx [1]byte
	if err := d.TxContext(context.Background(), 0, 0x20, nil, rx[:]); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.TxContext(ctx, 0, 0x20, nil, rx[:]); err != context.Canceled {
		t.Fatalf("canceled = %v", err)
	}
}

func TestNilBus(t *testing.T) {
	d := New(nil)
	if err := d.Init(); err != ErrNotConnected {
		t.Fatalf("nil bus = %v", err)
	}
	if _, err := d.Mask(); err != ErrNotConnected {
		t.Fatalf("Mask nil bus = %v", err)
	}
	if err := d.Select(0); err != ErrNotConnected {
		t.Fatalf("Select nil bus = %v", err)
	}
}

func TestConcurrent(t *testing.T) {
	a := &fakeDev{addr: 0x20, ch: 0}
	b := &fakeDev{addr: 0x21, ch: 1}
	bus := fakeBus(a, b)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(ch uint8) {
			defer wg.Done()
			var rx [1]byte
			for j := 0; j < 100; j++ {
				_ = d.Tx(ch, 0x20+uint16(ch), nil, rx[:])
				_ = d.Enable(ch)
				_, _ = d.Mask()
				_ = d.Disable(ch)
			}
		}(uint8(i))
	}
	wg.Wait()
	if bus.mask != 0 {
		t.Fatalf("mask after concurrency = %#02x, want 0", bus.mask)
	}
}

// TestDoRestoresOnError proves the defer: fn fails, the mask still comes back.
func TestDoRestoresOnError(t *testing.T) {
	d, bus := newDev(t)
	if err := d.Enable(0); err != nil {
		t.Fatal(err)
	}
	want := errors.New("work failed")
	if err := d.Do(2, func() error { return want }); err != want {
		t.Fatalf("Do error = %v, want %v", err, want)
	}
	if bus.mask != 0x01 {
		t.Fatalf("mask after failed Do = %#02x, want 0x01", bus.mask)
	}
	if err := d.Do(2, func() error { return nil }); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if bus.mask != 0x01 {
		t.Fatalf("mask after Do = %#02x, want 0x01", bus.mask)
	}
	if err := d.Do(NumChannels, func() error { return nil }); err != ErrBadChannel {
		t.Fatalf("Do(6) = %v", err)
	}
}

// TestDoNested uses two hubs, the case the unit was made for: the outer channel
// stays enabled while the inner hub runs, because Do restores instead of
// clearing.
func TestDoNested(t *testing.T) {
	outerBus := fakeBusAt(0x70)
	innerBus := fakeBusAt(0x71)
	dev := &fakeDev{addr: 0x29, ch: 3}
	innerBus.devs = []*fakeDev{dev}

	outer := New(outerBus)
	inner := NewAt2(innerBus, 0x71)
	if err := outer.Init(); err != nil {
		t.Fatal(err)
	}
	if err := inner.Init(); err != nil {
		t.Fatal(err)
	}

	var rx [1]byte
	err := outer.Do(2, func() error {
		return inner.Do(3, func() error {
			return innerBus.Tx(0x29, nil, rx[:])
		})
	})
	if err != nil {
		t.Fatalf("nested Do: %v", err)
	}
	if outerBus.mask != 0 || innerBus.mask != 0 {
		t.Fatalf("masks after nesting: outer %#02x inner %#02x", outerBus.mask, innerBus.mask)
	}
}

// TestTxCache covers the cache: a warm Tx costs 3 bus calls instead of 4, and
// Invalidate brings the read back.
func TestTxCache(t *testing.T) {
	dev := &fakeDev{addr: 0x20, ch: 2}
	bus := fakeBus(dev)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	var rx [1]byte

	before := bus.txCount
	if err := d.Tx(2, 0x20, nil, rx[:]); err != nil {
		t.Fatal(err)
	}
	if calls := bus.txCount - before; calls != 3 {
		t.Fatalf("warm Tx used %d bus calls, want 3 (write, device, restore)", calls)
	}

	d.Invalidate()
	before = bus.txCount
	if err := d.Tx(2, 0x20, nil, rx[:]); err != nil {
		t.Fatal(err)
	}
	if calls := bus.txCount - before; calls != 4 {
		t.Fatalf("invalidated Tx used %d bus calls, want 4", calls)
	}

	// A failed device transaction still restores and keeps the cache valid.
	if err := d.Tx(5, 0x20, nil, rx[:]); err == nil {
		t.Fatal("expected a NACK on the empty channel")
	}
	if got, _ := d.Mask(); got != 0 {
		t.Fatalf("mask after failed Tx = %#02x", got)
	}
}

// TestScanInto does not allocate when the caller provides capacity.
func TestScanInto(t *testing.T) {
	a := &fakeDev{addr: 0x20, ch: 1}
	b := &fakeDev{addr: 0x50, ch: 1}
	bus := fakeBus(a, b)
	d := New(bus)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}

	buf := make([]uint8, 0, 8)
	got, err := d.ScanInto(1, buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 0x20 || got[1] != 0x50 {
		t.Fatalf("ScanInto = %v", got)
	}

	// Reuse and no growth: the same backing array comes back.
	again, err := d.ScanInto(1, buf)
	if err != nil {
		t.Fatal(err)
	}
	if cap(again) != cap(buf) {
		t.Fatalf("capacity changed: %d -> %d (allocated)", cap(buf), cap(again))
	}

	allocs := testing.AllocsPerRun(20, func() {
		if _, err := d.ScanInto(1, buf); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("ScanInto allocates %v", allocs)
	}
}
