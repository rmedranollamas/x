package main

import (
	"fmt"
	"os"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/db"
	"github.com/spf13/cobra"
)

func newDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands.",
	}

	backupCmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of the database.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// If the database file does not exist on disk or is empty, print failure message
			if fi, err := os.Stat(cfg.DBPath()); os.IsNotExist(err) || (err == nil && fi.Size() == 0) {
				fmt.Fprintln(cmd.OutOrStdout(), "Backup failed or no database exists.")
				return nil
			}

			mgr, err := db.NewDBManager(cfg.DBPath())
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Backup failed or no database exists.")
				return nil
			}
			defer mgr.Close()

			backupPath, err := mgr.BackupDatabase()
			if err != nil || backupPath == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "Backup failed or no database exists.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Backup created at: %s\n", backupPath)
			return nil
		},
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show database configuration info.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			isDevStr := "False"
			if cfg.IsDev() {
				isDevStr = "True"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Environment: %s\n", cfg.Environment)
			fmt.Fprintf(cmd.OutOrStdout(), "Database File: %s\n", cfg.DBPath())
			fmt.Fprintf(cmd.OutOrStdout(), "Is Dev: %s\n", isDevStr)
			return nil
		},
	}

	cmd.AddCommand(backupCmd)
	cmd.AddCommand(infoCmd)

	return cmd
}
