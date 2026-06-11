package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile  string
	logLevel string
)

// rootCmd is the base command for cawa-server.
var rootCmd = &cobra.Command{
	Use:   "cawa-server",
	Short: "Arctic-tern Coding Agent Web Application Server",
	Long: `cawa-server starts the tern server which manages coding agent sessions
and provides an LLM gateway for model routing.`,
	RunE: runServer,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file path")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level (trace, debug, info, warn, error)")
}

func initConfig() {
	if cfgFile == "" {
		fmt.Fprintln(os.Stderr, "Error: config file path is required")
		os.Exit(1)
	}
}
