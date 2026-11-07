//go:build esp32

// Example: scan the six channels of the Unit PaHub v2.1 on the Grove port.
//
// The package has no board knowledge: you pass the I2C bus and the hub address.
// On an M5StickC Plus2 the Grove port A is machine.I2C1 on GPIO32/GPIO33.
package main

import (
	"time"

	"machine"

	pahub "github.com/nicolasmmb/M5Unit-PaHub-Go"
)

func main() {
	time.Sleep(time.Second)

	i2c := machine.I2C1
	if err := i2c.Configure(machine.I2CConfig{
		SDA:       machine.GPIO32,
		SCL:       machine.GPIO33,
		Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c:", err.Error())
		return
	}

	hub := pahub.New(i2c)
	if err := hub.Init(); err != nil {
		println("pahub:", err.Error())
		return
	}
	println("pahub at", hub.Address(), "ready")

	for {
		for ch := uint8(0); ch < pahub.NumChannels; ch++ {
			found, err := hub.Scan(ch)
			if err != nil {
				println("ch", ch, "err", err.Error())
				continue
			}
			print("ch ", ch, ":")
			if len(found) == 0 {
				print(" (empty)")
			}
			for _, addr := range found {
				print(" 0x")
				const hex = "0123456789abcdef"
				print(string(hex[addr>>4]), string(hex[addr&0xf]))
			}
			println()
		}
		println("---")
		time.Sleep(2 * time.Second)
	}
}
