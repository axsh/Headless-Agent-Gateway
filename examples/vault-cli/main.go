// vault-cli is a CLI tool for managing secrets in the HAG Vault.
// It directly accesses the OS Keyring via KeyringVaultBackend.
//
// Usage:
//
//	vault-cli <command> [options]
//
// Commands:
//
//	set       Store a secret (--provider <name> or --key <path>)
//	get       Check if a secret is registered (--provider <name> or --key <path>)
//	delete    Remove a secret (--provider <name> or --key <path>)
//	list      List vault keys
//	status    Show registration status of known LLM providers
//	version   Print version
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axsh/hag/vault"
)

const (
	appName    = "vault-cli"
	appVersion = "0.1.0"
)

// knownProviders are LLM providers checked by the "status" command.
var knownProviders = []string{"anthropic", "openai", "google"}

// ────────────────────────────────────────────────────────────
// Option structs
// ────────────────────────────────────────────────────────────

type setOptions struct {
	provider string
	key      string
	value    string // pre-set value (for --stdin or testing)
}

type getOptions struct {
	provider string
	key      string
	reveal   bool
}

type deleteOptions struct {
	provider string
	key      string
}

// ────────────────────────────────────────────────────────────
// Logic functions (testable, no os.Exit)
// ────────────────────────────────────────────────────────────

// runSetLogic stores a value in the VaultStore.
func runSetLogic(store vault.VaultStore, opts setOptions) error {
	fullKey := resolveKey(opts.provider, opts.key)
	if fullKey == "" {
		return fmt.Errorf("either --provider or --key is required")
	}
	if opts.value == "" {
		return fmt.Errorf("value is required")
	}
	if err := store.Set(fullKey, opts.value); err != nil {
		return fmt.Errorf("set failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Set: %s\n", fullKey)
	return nil
}

// runGetLogic checks if a key is registered.
func runGetLogic(store vault.VaultStore, opts getOptions, w io.Writer) error {
	fullKey := resolveKey(opts.provider, opts.key)
	if fullKey == "" {
		return fmt.Errorf("either --provider or --key is required")
	}
	val, err := store.Resolve("vault://" + fullKey)
	if err != nil {
		if opts.reveal {
			return fmt.Errorf("%s is not registered", fullKey)
		}
		fmt.Fprintf(w, "%s: not registered\n", fullKey)
		return nil
	}
	if opts.reveal {
		fmt.Fprint(w, val)
	} else {
		fmt.Fprintf(w, "%s: registered\n", fullKey)
	}
	return nil
}

// runDeleteLogic removes a key from the VaultStore.
func runDeleteLogic(store vault.VaultStore, opts deleteOptions) error {
	fullKey := resolveKey(opts.provider, opts.key)
	if fullKey == "" {
		return fmt.Errorf("either --provider or --key is required")
	}
	if err := store.Delete(fullKey); err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Deleted: %s\n", fullKey)
	return nil
}

// runListLogic lists keys from the VaultStore.
func runListLogic(store vault.VaultStore, w io.Writer) {
	paths, err := store.List()
	if err != nil {
		fmt.Fprintf(w, "Error listing keys: %v\n", err)
		return
	}
	if len(paths) == 0 {
		fmt.Fprintln(w, "No keys found")
		return
	}
	for _, p := range paths {
		fmt.Fprintf(w, "  %s\n", p)
	}
}

// runStatusLogic shows registration status of known LLM providers.
func runStatusLogic(store vault.VaultStore, w io.Writer) {
	fmt.Fprintln(w, "LLM Provider Status:")
	for _, p := range knownProviders {
		fullKey := "providers/" + p + "/default"
		_, err := store.Resolve("vault://" + fullKey)
		if err != nil {
			fmt.Fprintf(w, "  %s: not registered\n", p)
		} else {
			fmt.Fprintf(w, "  %s: registered\n", p)
		}
	}
}

// resolveKey converts --provider or --key to a full vault key path.
func resolveKey(provider, key string) string {
	if provider != "" {
		return "providers/" + provider + "/default"
	}
	return key
}

// ────────────────────────────────────────────────────────────
// CLI entry point
// ────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	cmdArgs := os.Args[2:]

	store := vault.NewKeyringVaultBackend()

	switch cmd {
	case "set":
		opts := parseSetArgs(cmdArgs)
		if err := runSetLogic(store, opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "get":
		opts := parseGetArgs(cmdArgs)
		if err := runGetLogic(store, opts, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "delete":
		opts := parseDeleteArgs(cmdArgs)
		if err := runDeleteLogic(store, opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "list":
		runListLogic(store, os.Stdout)
	case "status":
		runStatusLogic(store, os.Stdout)
	case "version":
		fmt.Printf("%s v%s\n", appName, appVersion)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// ────────────────────────────────────────────────────────────
// Argument parsers
// ────────────────────────────────────────────────────────────

func parseSetArgs(args []string) setOptions {
	var opts setOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 < len(args) {
				i++
				opts.provider = args[i]
			}
		case "--key":
			if i+1 < len(args) {
				i++
				opts.key = args[i]
			}
		case "--stdin":
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				opts.value = strings.TrimSpace(scanner.Text())
			}
		}
	}
	// If no --stdin, prompt interactively
	if opts.value == "" {
		fmt.Fprint(os.Stderr, "Enter value: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			opts.value = strings.TrimSpace(scanner.Text())
		}
	}
	return opts
}

func parseGetArgs(args []string) getOptions {
	var opts getOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 < len(args) {
				i++
				opts.provider = args[i]
			}
		case "--key":
			if i+1 < len(args) {
				i++
				opts.key = args[i]
			}
		case "--reveal":
			opts.reveal = true
		}
	}
	return opts
}

func parseDeleteArgs(args []string) deleteOptions {
	var opts deleteOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 < len(args) {
				i++
				opts.provider = args[i]
			}
		case "--key":
			if i+1 < len(args) {
				i++
				opts.key = args[i]
			}
		}
	}
	return opts
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `%s v%s — HAG Vault Secret Management CLI

Usage:
  %s <command> [options]

Commands:
  set       Store a secret
              --provider <name>   LLM provider (anthropic, openai)
              --key <path>        Custom vault key path
              --stdin             Read value from stdin (non-interactive)

  get       Check if a secret is registered
              --provider <name>   LLM provider
              --key <path>        Custom vault key path
              --reveal            Print the actual secret value

  delete    Remove a secret
              --provider <name>   LLM provider
              --key <path>        Custom vault key path

  list      List vault keys (keys only, no values)

  status    Show registration status of known LLM providers
  version   Print version
  help      Show this help
`, appName, appVersion, appName)
}
