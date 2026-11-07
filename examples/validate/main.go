//go:build esp32

// Validation firmware: one test per flash, selected in config.go.
//
//	1  presence: read the control register, expect 0x00 after Init
//	2  select:   walk channels 0..5, read the mask back each time
//	3  scan:     scan all six channels and print every address
//	4  restore:  enable ch0, transact on ch2, check the mask comes back
//	5  nesting:  enable two bits, confirm both stay set
package main

import (
	"time"

	"machine"

	pahub "github.com/nicolasmmb/M5Unit-PaHub-Go"
)

func main() {
	time.Sleep(800 * time.Millisecond)

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
	println("TEST", test, "-", testName(test))

	// A máscara do PCA9548A sobrevive ao reset do MCU: o unit continua
	// alimentado pelo Grove. Sempre começar de um estado conhecido.
	if err := hub.Init(); err != nil {
		println("FAIL init:", err.Error())
		return
	}
	if mask, err := hub.Mask(); err == nil {
		println("mask at boot:", mask, "(after Init)")
	}

	for {
		switch test {
		case 1:
			if err := hub.Init(); err != nil {
				println("FAIL init:", err.Error())
				break
			}
			mask, err := hub.Mask()
			println("mask after Init:", mask, "err:", err)
			if err == nil && mask == 0 {
				println("PASS presence")
			} else {
				println("FAIL presence")
			}

		case 2:
			ok := true
			for ch := uint8(0); ch < pahub.NumChannels; ch++ {
				if err := hub.Select(ch); err != nil {
					println("FAIL select", ch, err.Error())
					ok = false
					continue
				}
				mask, err := hub.Mask()
				want := uint8(1) << ch
				println("select", ch, "-> mask", mask, "want", want, "err", err)
				if err != nil || mask != want {
					ok = false
				}
			}
			if ok {
				println("PASS select")
			} else {
				println("FAIL select")
			}

		case 3:
			for ch := uint8(0); ch < pahub.NumChannels; ch++ {
				found, err := hub.Scan(ch)
				if err != nil {
					println("ch", ch, "err", err.Error())
					continue
				}
				print("ch ", ch, ":", len(found), "dev")
				for _, a := range found {
					print(" 0x")
					const hex = "0123456789abcdef"
					print(string(hex[a>>4]), string(hex[a&0xf]))
				}
				println()
			}
			println("PASS scan")

		case 4:
			if err := hub.Enable(0); err != nil {
				println("FAIL enable:", err.Error())
				break
			}
			var rx [1]byte
			_ = hub.Tx(2, 0x20, nil, rx[:]) // esperado falhar sem device
			mask, _ := hub.Mask()
			println("mask after Tx on ch2:", mask, "(want 1)")
			if mask == 1 {
				println("PASS restore")
			} else {
				println("FAIL restore")
			}
			_ = hub.Close()

		case 5:
			_ = hub.Close()
			_ = hub.Enable(0)
			_ = hub.Enable(3)
			mask, _ := hub.Mask()
			println("mask with ch0+ch3 enabled:", mask, "(want 9)")
			if mask == 9 {
				println("PASS nesting")
			} else {
				println("FAIL nesting")
			}
			_ = hub.Close()

		default:
			println("no test selected")
		}
		time.Sleep(3 * time.Second)
	}
}

func testName(t int) string {
	switch t {
	case 1:
		return "presence"
	case 2:
		return "select each channel"
	case 3:
		return "scan all channels"
	case 4:
		return "restore mask after Tx"
	case 5:
		return "nesting: two bits enabled"
	}
	return "idle"
}
