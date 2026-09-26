package main

import (
	"errors"
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/spf13/cobra"
)

// ErrConfigValidation indicates that configuration validation failed.
var ErrConfigValidation = errors.New("configuration validation failed")

// Persistent global flags.
var (
	debugFlag bool
)

// Global root command instance.
var rootCmd = newRootCmd()

func newRootCmd() *cobra.Command {
	debugFlag = false

	cmd := &cobra.Command{
		Use:           "x-agent",
		Short:         "A command-line tool to manage X interactions with agents.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Bypass validation for help invocations.
			if cmd.Name() == "help" || cmd.CalledAs() == "help" || cmd.Flags().Changed("help") {
				return nil
			}

			// Load configuration from .env and environment variables.
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Configuration Error: %v\n", err)
				return fmt.Errorf("%w: %v", ErrConfigValidation, err)
			}

			// Validate core API credentials.
			if err := cfg.CheckConfig(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Configuration Error: %v\n", err)
				return fmt.Errorf("%w: %v", ErrConfigValidation, err)
			}

			// Output startup visibility header to stderr.
			if err := logging.PrintStartupHeader(cmd.ErrOrStderr(), cfg.Environment, cfg.IsDev(), cfg.DBName()); err != nil {
				return err
			}

			// Configure logging level.
			if debugFlag {
				logging.SetDebug(true)
			}

			return nil
		},
	}

	cmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Enable debug logging for detailed output.")

	// Register all subcommands.
	cmd.AddCommand(newUnblockCmd())
	cmd.AddCommand(newInsightsCmd())
	cmd.AddCommand(newBlockedIDsCmd())
	cmd.AddCommand(newUnfollowCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newDBCmd())

	return cmd
}

// Execute runs the root command.
func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		if !errors.Is(err, ErrConfigValidation) {
			fmt.Fprintf(rootCmd.ErrOrStderr(), "Error: %v\n", err)
		}
		return err
	}
	return nil
}
