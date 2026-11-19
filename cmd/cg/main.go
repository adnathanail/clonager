package main

import (
	"os"

	"github.com/adnathanail/clonager/internal/commands"
)

func main() {
	os.Exit(commands.Execute())
}
