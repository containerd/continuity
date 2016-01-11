package main

import (
	_ "crypto/sha256"

	"github.com/stevvooe/continuity/commands"
)

func main() {
	commands.MainCmd.Execute()
}
