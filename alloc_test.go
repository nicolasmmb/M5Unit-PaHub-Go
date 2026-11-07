package pahub

import (
	"testing"
	"unsafe"
)

// silentBus answers like a live hub and keeps no history, so it does not
// allocate during a benchmark or an allocation contract test.
type silentBus struct{ mask uint8 }

func (b *silentBus) Tx(addr uint16, w, r []byte) error {
	if len(w) > 0 {
		b.mask = w[0] & 0x3f
	}
	if len(r) > 0 {
		r[0] = b.mask
	}
	return nil
}

func TestNoAlloc(t *testing.T) {
	bus := &silentBus{}
	d := New(bus)
	var scratch [1]byte
	allocs := testing.AllocsPerRun(200, func() {
		_ = d.Select(3)
		_ = d.Enable(4)
		_ = d.Disable(4)
		_, _ = d.Mask()
		_ = d.Tx(2, 0x20, nil, scratch[:])
		_ = d.SelectAll(0x3f)
		_ = d.Close()
	})
	if allocs != 0 {
		t.Fatalf("driver path allocates %v per run", allocs)
	}
}

func TestFootprint(t *testing.T) {
	size := unsafe.Sizeof(Device{})
	t.Logf("Device=%d B", size)
	if size > 48 {
		t.Fatalf("Device is %d B, budget is 48 B", size)
	}
}
