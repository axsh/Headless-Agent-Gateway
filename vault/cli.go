package vault

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	defaultAppName    = "vault-cli"
	defaultAppVersion = "0.1.0"
)

var defaultKnownProviders = []string{"anthropic", "openai", "google"}

type setOptions struct {
	provider string
	key      string
	value    string
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

// CLIRunner provides a high-level, embeddable CLI facade for Vault operations.
type CLIRunner struct {
	cfg CLIConfig
	svc *Service
}

// NewCLIRunner creates a new CLIRunner with sane defaults.
func NewCLIRunner(cfg CLIConfig) *CLIRunner {
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if len(cfg.KnownProviders) == 0 {
		cfg.KnownProviders = append([]string(nil), defaultKnownProviders...)
	}
	if cfg.AppName == "" {
		cfg.AppName = defaultAppName
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = defaultAppVersion
	}
	return &CLIRunner{
		cfg: cfg,
		svc: NewService(cfg.Store),
	}
}

// Run executes CLI commands and returns process-style exit code.
func (r *CLIRunner) Run(args []string) int {
	if r == nil || r.svc == nil {
		return 1
	}
	if len(args) < 1 {
		r.printUsage()
		return 1
	}
	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "set":
		opts := parseSetArgs(cmdArgs, r.cfg.Stdin, r.cfg.Stderr)
		fullKey, err := r.svc.Set(opts.provider, opts.key, opts.value)
		if err != nil {
			fmt.Fprintf(r.cfg.Stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(r.cfg.Stderr, "Set: %s\n", fullKey)
		return 0
	case "get":
		opts := parseGetArgs(cmdArgs)
		res, err := r.svc.Get(opts.provider, opts.key, opts.reveal)
		if err != nil {
			fmt.Fprintf(r.cfg.Stderr, "Error: %v\n", err)
			return 1
		}
		if opts.reveal {
			fmt.Fprint(r.cfg.Stdout, res.Value)
		} else if res.Registered {
			fmt.Fprintf(r.cfg.Stdout, "%s: registered\n", res.FullKey)
		} else {
			fmt.Fprintf(r.cfg.Stdout, "%s: not registered\n", res.FullKey)
		}
		return 0
	case "delete":
		opts := parseDeleteArgs(cmdArgs)
		fullKey, err := r.svc.Delete(opts.provider, opts.key)
		if err != nil {
			fmt.Fprintf(r.cfg.Stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(r.cfg.Stderr, "Deleted: %s\n", fullKey)
		return 0
	case "list":
		paths, err := r.svc.List()
		if err != nil {
			fmt.Fprintf(r.cfg.Stdout, "Error listing keys: %v\n", err)
			return 1
		}
		if len(paths) == 0 {
			fmt.Fprintln(r.cfg.Stdout, "No keys found")
			return 0
		}
		for _, p := range paths {
			fmt.Fprintf(r.cfg.Stdout, "  %s\n", p)
		}
		return 0
	case "status":
		status, err := r.svc.Status(r.cfg.KnownProviders)
		if err != nil {
			fmt.Fprintf(r.cfg.Stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(r.cfg.Stdout, "LLM Provider Status:")
		for _, st := range status {
			state := "not registered"
			if st.Registered {
				state = "registered"
			}
			fmt.Fprintf(r.cfg.Stdout, "  %s: %s\n", st.Provider, state)
		}
		return 0
	case "version":
		fmt.Fprintf(r.cfg.Stdout, "%s v%s\n", r.cfg.AppName, r.cfg.AppVersion)
		return 0
	case "help", "--help", "-h":
		r.printUsage()
		return 0
	default:
		fmt.Fprintf(r.cfg.Stderr, "Unknown command: %s\n", cmd)
		r.printUsage()
		return 1
	}
}

func parseSetArgs(args []string, in io.Reader, promptOut io.Writer) setOptions {
	var opts setOptions
	readFromStdin := false
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
			readFromStdin = true
		}
	}
	if readFromStdin {
		opts.value = readSingleLine(in)
		return opts
	}
	fmt.Fprint(promptOut, "Enter value: ")
	opts.value = readSingleLine(in)
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

func readSingleLine(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func (r *CLIRunner) printUsage() {
	fmt.Fprintf(r.cfg.Stderr, `%s v%s — tern Vault Secret Management CLI

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
`, r.cfg.AppName, r.cfg.AppVersion, r.cfg.AppName)
}
