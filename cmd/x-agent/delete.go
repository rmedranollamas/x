package main

import (
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/agents"
	"github.com/spf13/cobra"
)

type deleteOptions struct {
	dryRun       bool
	email        bool
	protectedIDs []int64
	archive      string
	days         int
	minViews     int
	keepPinned   bool
	maxDelete    int
}

func newDeleteCmd() *cobra.Command {
	opts := &deleteOptions{
		days:       30,
		minViews:   100,
		keepPinned: true,
	}

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Run the delete agent to remove old tweets based on engagement rules.",
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

			agentOpts := agents.DeleteOptions{
				ArchivePath:  opts.archive,
				ProtectedIDs: opts.protectedIDs,
				DryRun:       opts.dryRun,
				Email:        opts.email,
				Days:         opts.days,
				MinViews:     opts.minViews,
				KeepPinned:   opts.keepPinned,
				MaxDelete:    opts.maxDelete,
			}

			agent := agents.NewDeleteAgent(client, dbMgr, cfg, agentOpts)
			return agent.Run(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Simulate actions without making changes.")
	cmd.Flags().BoolVar(&opts.email, "email", false, "Send the report via email after generation.")
	cmd.Flags().Int64SliceVar(&opts.protectedIDs, "protected-id", nil, "Tweet IDs to protect from deletion.")
	cmd.Flags().StringVar(&opts.archive, "archive", "", "Path to X data archive (tweets.js).")
	cmd.Flags().IntVar(&opts.days, "days", 30, "Cutoff age threshold for tweet pruning.")
	cmd.Flags().IntVar(&opts.minViews, "min-views", 100, "Minimum views to retain tweet.")
	cmd.Flags().BoolVar(&opts.keepPinned, "keep-pinned", true, "Protect pinned tweet from deletion.")
	cmd.Flags().IntVar(&opts.maxDelete, "max-delete", 0, "Maximum number of tweets to delete (0 = unlimited).")

	return cmd
}
