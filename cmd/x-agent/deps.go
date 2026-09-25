package main

import (
	"fmt"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/db"
	"github.com/rmedranollamas/x-agent/internal/xapi"
	"github.com/spf13/cobra"
)

// initAgentDeps loads configuration, applies database migrations, and initializes the Twitter API client.
func initAgentDeps(cmd *cobra.Command) (*config.Config, *db.DBManager, xapi.XClient, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("configuration load error: %w", err)
	}

	if err := cfg.CheckConfig(); err != nil {
		return nil, nil, nil, fmt.Errorf("configuration validation error: %w", err)
	}

	dbMgr, err := db.NewDBManager(cfg.DBPath())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("database connection error: %w", err)
	}

	// Guarantee schema migrations m001-m004 are applied idempotently
	if err := dbMgr.RunMigrations(); err != nil {
		dbMgr.Close()
		return nil, nil, nil, fmt.Errorf("database migration error: %w", err)
	}

	client, err := xapi.NewClient(cfg)
	if err != nil {
		dbMgr.Close()
		return nil, nil, nil, fmt.Errorf("twitter client initialization error: %w", err)
	}

	return cfg, dbMgr, client, nil
}
