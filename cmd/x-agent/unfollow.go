package main

import (
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/agents"
	"github.com/spf13/cobra"
)

type unfollowOptions struct {
	dryRun bool
	email  bool
}

func newUnfollowCmd() *cobra.Command {
	opts := &unfollowOptions{}

	cmd := &cobra.Command{
		Use:   "unfollow",
		Short: "Run the unfollow agent to detect who has unfollowed you.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, dbMgr, client, err := initAgentDeps(cmd)
			if err != nil {
				return err
			}
			defer dbMgr.Close()

			if opts.email {
				if err := cfg.CheckEmailConfig(); err != nil {
					return fmt.Errorf("email configuration error: %w", err)
				}
			}

			agent := agents.NewUnfollowAgent(
				client,
				dbMgr,
				cfg,
				agents.UnfollowOptions{
					DryRun: opts.dryRun,
					Email:  opts.email,
				},
				cmd.OutOrStdout(),
			)

			return agent.Run(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Simulate actions without making changes.")
	cmd.Flags().BoolVar(&opts.email, "email", false, "Send the report via email after generation.")

	return cmd
}
