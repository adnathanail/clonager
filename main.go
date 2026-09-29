package main

import (
	"os"

	"github.com/adnathanail/clonager/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
