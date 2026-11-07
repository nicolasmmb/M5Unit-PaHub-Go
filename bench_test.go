package pahub

import "testing"

func benchDevice() (*Device, *silentBus) {
	bus := &silentBus{}
	return New(bus), bus
}

// BenchmarkTx is the cost of one full transaction: read mask, set channel,
// bus call, restore mask. Four bus transactions in total.
func BenchmarkTx(b *testing.B) {
	d, _ := benchDevice()
	var scratch [1]byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.Tx(2, 0x20, nil, scratch[:])
	}
}

// BenchmarkSelect is the cheapest command: one write.
func BenchmarkSelect(b *testing.B) {
	d, _ := benchDevice()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.Select(uint8(i % NumChannels))
	}
}

// BenchmarkEnable is read-modify-write: two transactions.
func BenchmarkEnable(b *testing.B) {
	d, _ := benchDevice()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.Enable(uint8(i % NumChannels))
	}
}

// BenchmarkMask is the presence probe: one read.
func BenchmarkMask(b *testing.B) {
	d, _ := benchDevice()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.Mask()
	}
}
