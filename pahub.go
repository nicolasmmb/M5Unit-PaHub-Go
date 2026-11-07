// Package pahub drives the M5Stack Unit PaHub v2.1 (SKU U040-B-V21), an I2C
// multiplexer built on the PCA9548A. The older Unit PaHub (U040) uses the
// TCA9548A, which has the same control register, so this package drives both.
//
// The unit expands one Grove I2C port into six channels. Only the selected
// channels reach the main bus, so two devices with the same address can coexist.
//
// The package knows nothing about boards: it depends only on the two-argument
// I2C interface, so machine.I2C satisfies it on any chip, and a fake bus makes
// the whole driver testable on the host.
//
// One byte of the device is the control register: bit N enables channel N.
// After power-on every channel is disabled.
package pahub

import (
	"context"
	"errors"
	"sync"
)

// I2C is the subset of machine.I2C used by the driver.
type I2C interface {
	// Tx writes w to addr, then reads len(r) bytes into r.
	// A one-byte read with an empty w is a presence probe.
	Tx(addr uint16, w, r []byte) error
}

const (
	// DefaultAddress is the factory address of the PaHub. The DIP switch on
	// the v2.1 (solder pads on the v1) moves it to 0x70..0x77.
	DefaultAddress = 0x70

	// NumChannels is the number of Grove ports of the unit. The PCA9548A has
	// eight channels; the unit exposes six.
	NumChannels = 6

	// firstAddress and lastAddress bound the valid hub addresses.
	firstAddress = 0x70
	lastAddress  = 0x7f
)

// Errors returned by the driver. They are sentinels: compare with errors.Is.
var (
	ErrNotConnected = errors.New("pahub: hub not responding")
	ErrBadAddress   = errors.New("pahub: address must be 0x70..0x7f")
	ErrBadChannel   = errors.New("pahub: channel must be below 6")
)

// Device is one PaHub. It is safe for concurrent use: every operation takes a
// mutex because the channel mask is global state of the I2C bus.
type Device struct {
	mu   sync.Mutex
	bus  I2C
	addr uint16
	tx   [1]byte // scratch for the control register write, so Tx does not allocate
	rx   [1]byte // scratch for the read-back
}

// New returns a Device at the default address. Call Init to check presence.
func New(bus I2C) *Device {
	return &Device{bus: bus, addr: DefaultAddress}
}

// NewAt returns a Device at addr (0x70..0x7f) and checks that the hub answers.
func NewAt(bus I2C, addr uint16) (*Device, error) {
	d := &Device{bus: bus, addr: addr}
	if _, err := d.Mask(); err != nil {
		return nil, err
	}
	return d, nil
}

// Address returns the configured I2C address.
func (d *Device) Address() uint16 { return d.addr }

// Init disables every channel and confirms the hub answers.
//
// The PCA9548A has no identification register, so presence is checked by
// writing the control register and reading it back.
func (d *Device) Init() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.validateLocked(); err != nil {
		return err
	}
	if err := d.writeMaskLocked(0); err != nil {
		return err
	}
	mask, err := d.readMaskLocked()
	if err != nil {
		return err
	}
	if mask != 0 {
		return ErrNotConnected
	}
	return nil
}

// Select enables exactly one channel and disables the others, like portselect
// in the official Arduino example.
func (d *Device) Select(channel uint8) error {
	if err := checkChannel(channel); err != nil {
		return err
	}
	return d.SelectAll(1 << channel)
}

// SelectAll writes the channel mask directly, like portselectall in the
// official example. Bits above the six channels are masked out.
func (d *Device) SelectAll(mask uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writeMaskLocked(mask & channelMask)
}

// Enable turns one channel on and keeps the others as they are, like
// select_channel in the UIFlow driver.
func (d *Device) Enable(channel uint8) error {
	if err := checkChannel(channel); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	mask, err := d.readMaskLocked()
	if err != nil {
		return err
	}
	return d.writeMaskLocked(mask | 1<<channel)
}

// Disable turns one channel off and keeps the others as they are, like
// release_channel in the UIFlow driver.
func (d *Device) Disable(channel uint8) error {
	if err := checkChannel(channel); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	mask, err := d.readMaskLocked()
	if err != nil {
		return err
	}
	return d.writeMaskLocked(mask &^ (1 << channel))
}

// Mask reads the control register. It is also the presence probe.
func (d *Device) Mask() (uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.readMaskLocked()
}

// Close disables every channel.
func (d *Device) Close() error { return d.SelectAll(0) }

// Tx runs one I2C transaction on channel. It saves the current mask, enables
// the channel, runs the transaction and restores the saved mask.
//
// Restoring (instead of clearing the bit, as the UIFlow driver does) keeps the
// channels that other code enabled, which is what makes nested hubs work.
func (d *Device) Tx(channel uint8, addr uint16, w, r []byte) error {
	if err := checkChannel(channel); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.txLocked(channel, addr, w, r)
}

// TxContext is Tx with a cancellation check before the bus transaction.
func (d *Device) TxContext(ctx context.Context, channel uint8, addr uint16, w, r []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkChannel(channel); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.txLocked(channel, addr, w, r)
}

// Scan lists the addresses that answer on channel, skipping the hub itself.
// It allocates the result, so do not call it in a control loop.
func (d *Device) Scan(channel uint8) ([]uint8, error) {
	if err := checkChannel(channel); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	var found []uint8
	err := d.doLocked(channel, func() error {
		for addr := uint16(0x08); addr <= 0x77; addr++ {
			if addr == d.addr {
				continue
			}
			if d.bus.Tx(addr, nil, d.rx[:1]) == nil {
				found = append(found, uint8(addr))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// Do enables channel, runs fn and restores the previous mask with defer, so
// the mask comes back even when fn fails or a later edit adds an early return.
//
// It is the building block for nested hubs: two Devices have separate mutexes,
// so hubA.Do(2, func() error { return hubB.Do(3, work) }) keeps both channels
// enabled for work.
//
// fn runs with the device locked, so it must not call methods of this Device
// (only methods of another Device are safe).
func (d *Device) Do(channel uint8, fn func() error) error {
	if err := checkChannel(channel); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.doLocked(channel, fn)
}

// doLocked is the shared select/run/restore path. The caller holds the mutex.
// The named return keeps the restore error when fn itself succeeded.
func (d *Device) doLocked(channel uint8, fn func() error) (err error) {
	saved, err := d.readMaskLocked()
	if err != nil {
		return err
	}
	if err = d.writeMaskLocked(saved | 1<<channel); err != nil {
		return err
	}
	defer func() {
		if restoreErr := d.writeMaskLocked(saved); err == nil {
			err = restoreErr
		}
	}()
	return fn()
}

func (d *Device) txLocked(channel uint8, addr uint16, w, r []byte) error {
	return d.doLocked(channel, func() error {
		return d.bus.Tx(addr, w, r)
	})
}

// readMaskLocked reads the control register. The caller holds the mutex.
func (d *Device) readMaskLocked() (uint8, error) {
	if err := d.validateLocked(); err != nil {
		return 0, err
	}
	if err := d.bus.Tx(d.addr, nil, d.rx[:1]); err != nil {
		return 0, ErrNotConnected
	}
	return d.rx[0] & channelMask, nil
}

// writeMaskLocked writes the control register. The caller holds the mutex.
func (d *Device) writeMaskLocked(mask uint8) error {
	if err := d.validateLocked(); err != nil {
		return err
	}
	d.tx[0] = mask
	if err := d.bus.Tx(d.addr, d.tx[:1], nil); err != nil {
		return ErrNotConnected
	}
	return nil
}

func (d *Device) validateLocked() error {
	if d.bus == nil {
		return ErrNotConnected
	}
	if d.addr < firstAddress || d.addr > lastAddress {
		return ErrBadAddress
	}
	return nil
}

// channelMask keeps the low six bits.
const channelMask = 1<<NumChannels - 1

func checkChannel(channel uint8) error {
	if channel >= NumChannels {
		return ErrBadChannel
	}
	return nil
}
