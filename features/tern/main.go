package main

import (
	"fmt"
	"os"

	"github.com/axsh/arctic-tern/features/tern/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
