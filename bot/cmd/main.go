package main

import (
	"fmt"
	"os"

	"github.com/sibstark/athena/bot/internal/config"
)

func main() {
	err := run()
	if err != nil {
		fmt.Printf("init bot run err %s", err.Error())
		os.Exit(1)
	}
}

func run() error {
	_, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration err %w", err)
	}
	return nil
}
