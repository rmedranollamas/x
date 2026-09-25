package main

import (
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/agents"
	"github.com/spf13/cobra"
)

type insightsOptions struct {
	email bool
}

func newInsightsCmd() *cobra.Command {
	opts := &insightsOptions{}

	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Run the insights agent to gather and report account metrics.",
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

			agent := agents.NewInsightsAgent(
				client,
				dbMgr,
				cfg,
				agents.InsightsOptions{
					Email: opts.email,
				},
				cmd.OutOrStdout(),
			)

			return agent.Run(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&opts.email, "email", false, "Send the report via email after generation.")

	return cmd
}
