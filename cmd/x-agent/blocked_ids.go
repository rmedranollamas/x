package main

import (
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/agents"
	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/xapi"
	"github.com/spf13/cobra"
)

func newBlockedIDsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blocked-ids",
		Short: "Run the blocked-ids agent to fetch and print blocked user IDs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.CheckConfig(); err != nil {
				return err
			}

			client, err := xapi.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("initialize client: %w", err)
			}

			agent := agents.NewBlockedIDsAgent(client, cmd.OutOrStdout())
			return agent.Run(cmd.Context())
		},
	}

	return cmd
}
