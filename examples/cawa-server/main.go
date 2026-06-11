package main

import (
	"fmt"
	"os"

	"github.com/axsh/arctic-tern/examples/cawa-server/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
