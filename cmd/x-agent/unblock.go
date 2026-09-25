package main

import (
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/agents"
	"github.com/spf13/cobra"
)

type unblockOptions struct {
	userID  int64
	refresh bool
	dryRun  bool
}

func newUnblockCmd() *cobra.Command {
	opts := &unblockOptions{}

	cmd := &cobra.Command{
		Use:   "unblock",
		Short: "Run the unblock agent to unblock blocked accounts.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("user-id") && opts.userID <= 0 {
				return fmt.Errorf("user-id must be a positive integer")
			}

			_, dbMgr, client, err := initAgentDeps(cmd)
			if err != nil {
				return err
			}
			defer dbMgr.Close()

			var targetUserID *int64
			if cmd.Flags().Changed("user-id") {
				targetUserID = &opts.userID
			}

			agent := agents.NewUnblockAgent(client, dbMgr, agents.UnblockOptions{
				UserID:  targetUserID,
				Refresh: opts.refresh,
				DryRun:  opts.dryRun,
			})
			return agent.Run(cmd.Context())
		},
	}

	cmd.Flags().Int64Var(&opts.userID, "user-id", 0, "Optional: Specify a single user ID to unblock.")
	cmd.Flags().BoolVar(&opts.refresh, "refresh", false, "Re-fetch blocked IDs from API, ignoring local cache.")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Simulate actions without making changes.")

	return cmd
}
