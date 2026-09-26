package agents

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// BlockedIDsAgent coordinates fetching blocked account IDs and streaming to standard output.
type BlockedIDsAgent struct {
	client xapi.XClient
	out    io.Writer
}

// NewBlockedIDsAgent constructs a new BlockedIDsAgent.
// If out is nil, os.Stdout is used by default.
func NewBlockedIDsAgent(client xapi.XClient, out io.Writer) *BlockedIDsAgent {
	if out == nil {
		out = os.Stdout
	}
	return &BlockedIDsAgent{
		client: client,
		out:    out,
	}
}

// Run fetches and streams blocked user IDs to standard output.
func (a *BlockedIDsAgent) Run(ctx context.Context) error {
	logging.Info("--- X Blocked IDs Agent ---")

	apiBlockedIDs, err := a.client.GetBlockedUserIDs(ctx)
	if err != nil {
		logging.Errorf("Failed to fetch blocked user IDs: %v", err)
		return err
	}

	if len(apiBlockedIDs) > 0 {
		logging.Infof("Found %d blocked user IDs:", len(apiBlockedIDs))
		for _, userID := range apiBlockedIDs {
			if _, err := fmt.Fprintf(a.out, "%d\n", userID); err != nil {
				return fmt.Errorf("failed writing blocked ID to output: %w", err)
			}
		}
	} else {
		logging.Info("No blocked IDs found from the API.")
	}

	return nil
}
