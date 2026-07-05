package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/B83C/t57-go/internal/serial"
	"github.com/B83C/t57-go/internal/t57"
)

func main() {
	port := "/dev/ttyACM3"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	baud := 9600
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &baud)
	}

	fmt.Printf("Opening %s @ %d baud ...\n", port, baud)
	tr, err := serial.OpenWithTimeout(port, baud, 2*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer tr.Close()

	c := t57.NewClient(tr).WithRetries(1)

	// Signal handler for clean exit
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Connected. Sending SysGetSerlNum...")
	sn, err := c.SerialNumber()
	if err != nil {
		fmt.Fprintf(os.Stderr, "SerialNumber: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Serial: %X\n", sn)

	fmt.Println("Reading firmware version...")
	v, err := c.FirmwareVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Version: %v\n", err)
	} else {
		fmt.Printf("Firmware: %s\n", strings.TrimRight(string(v), "\x00 "))
	}

	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()

	cycle := 0
	for {
		select {
		case <-sig:
			fmt.Println("\nExiting.")
			return
		case <-tick.C:
			cycle++
			fmt.Printf("\n--- Cycle %d ---\n", cycle)

			// Read block 1
			b1, err := c.ReadBlock(1)
			if err != nil {
				fmt.Printf("read block 1: %v\n", err)
				continue
			}
			fmt.Printf("block 1: %02X%02X%02X%02X\n", b1[0], b1[1], b1[2], b1[3])

			// Read block 2
			b2, err := c.ReadBlock(2)
			if err != nil {
				fmt.Printf("read block 2: %v\n", err)
				continue
			}
			fmt.Printf("block 2: %02X%02X%02X%02X\n", b2[0], b2[1], b2[2], b2[3])

			// Read config
			cfg, err := c.ReadConfig()
			if err != nil {
				fmt.Printf("read config: %v\n", err)
				continue
			}
			raw := cfg.LEBytes()
			fmt.Printf("config: %02X%02X%02X%02X\n", raw[0], raw[1], raw[2], raw[3])

			fmt.Println("OK")
		}
	}
}
