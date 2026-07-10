package main

import (
	"os"

	sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
	apivault "github.com/axsh/arctic-tern/vault"
)

func main() {
	runner := apivault.NewCLIRunner(apivault.CLIConfig{
		Store:      sharedvault.NewKeyringVaultBackend(),
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		AppName:    "vault-cli",
		AppVersion: "0.1.0",
	})
	os.Exit(runner.Run(os.Args[1:]))
}
