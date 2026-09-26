package agents

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// BlockedUsersDB defines the SQLite operations required by UnblockAgent.
type BlockedUsersDB interface {
	GetAllBlockedUsersCount(ctx context.Context) (int, error)
	ClearPendingBlockedUsers(ctx context.Context) error
	AddBlockedUsers(ctx context.Context, userIDs []int64) error
	UpsertBlockedUsers(ctx context.Context, userIDs []int64) error
	GetPendingBlockedUsers(ctx context.Context) ([]int64, error)
	UpdateBlockedUserStatus(ctx context.Context, userID int64, status string) error
	UpdateBlockedUserStatuses(ctx context.Context, userIDs []int64, status string) error
}

// UnblockOptions holds runtime configuration flags for UnblockAgent.
type UnblockOptions struct {
	UserID  *int64
	Refresh bool
	DryRun  bool
}

// UnblockAgent manages the unblocking workflow with zombie recovery and batching.
type UnblockAgent struct {
	client    xapi.XClient
	db        BlockedUsersDB
	opts      UnblockOptions
	batchSize int
	workers   int
}

// NewUnblockAgent constructs a new UnblockAgent with standard batching parameters.
func NewUnblockAgent(client xapi.XClient, db BlockedUsersDB, opts UnblockOptions) *UnblockAgent {
	return &UnblockAgent{
		client:    client,
		db:        db,
		opts:      opts,
		batchSize: 50,
		workers:   20,
	}
}

// Run executes the unblocking workflow.
func (a *UnblockAgent) Run(ctx context.Context) error {
	if a.opts.DryRun {
		logging.Info("DRY RUN ENABLED: No changes will be made to X.")
	}

	// 1. Single user ID override
	if a.opts.UserID != nil {
		return a.runSingleUser(ctx, *a.opts.UserID)
	}

	// 2. Batch unblock process
	return a.runBatch(ctx)
}

func (a *UnblockAgent) runSingleUser(ctx context.Context, userID int64) error {
	logging.Infof("Attempting to unblock specific user ID: %d", userID)

	if a.opts.DryRun {
		logging.Infof("[Dry Run] Would unblock %d", userID)
		return nil
	}

	status, err := a.client.UnblockUser(ctx, userID)

	switch {
	case status == "UNBLOCKED" || status == "SUCCESS":
		logging.Infof("Successfully unblocked %d.", userID)
		if a.db != nil {
			_ = a.db.UpsertBlockedUsers(ctx, []int64{userID})
			if err := a.db.UpdateBlockedUserStatus(ctx, userID, "UNBLOCKED"); err != nil {
				logging.Warnf("Failed to update status in database for user %d: %v", userID, err)
			}
		}
		return nil

	case status == "NOT_FOUND":
		logging.Infof("User ID %d not found (deleted/suspended).", userID)
		if a.db != nil {
			_ = a.db.UpsertBlockedUsers(ctx, []int64{userID})
			if err := a.db.UpdateBlockedUserStatus(ctx, userID, "NOT_FOUND"); err != nil {
				logging.Warnf("Failed to update status in database for user %d: %v", userID, err)
			}
		}
		return nil

	default:
		logging.Errorf("Failed to unblock %d: %s", userID, status)
		if a.db != nil {
			_ = a.db.UpsertBlockedUsers(ctx, []int64{userID})
			if err := a.db.UpdateBlockedUserStatus(ctx, userID, "FAILED"); err != nil {
				logging.Warnf("Failed to update status in database for user %d: %v", userID, err)
			}
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("unblock failed for user %d: %s", userID, status)
	}
}

func (a *UnblockAgent) runBatch(ctx context.Context) error {
	logging.Info("--- X Unblock Agent (Async) ---")

	var totalBlockedCount int
	var err error
	if a.db != nil {
		totalBlockedCount, err = a.db.GetAllBlockedUsersCount(ctx)
		if err != nil {
			return fmt.Errorf("failed to get blocked users count: %w", err)
		}
	}

	var allBlockedIDs []int64
	if totalBlockedCount == 0 || a.opts.Refresh {
		if a.opts.Refresh {
			logging.Info("Refresh requested. Fetching latest blocked IDs from API...")
			if a.db != nil && !a.opts.DryRun {
				if err := a.db.ClearPendingBlockedUsers(ctx); err != nil {
					return fmt.Errorf("failed to clear pending blocked users: %w", err)
				}
			}
		} else {
			logging.Info("No local cache of blocked IDs found. Fetching from the API...")
		}

		allBlockedIDs, err = a.client.GetBlockedUserIDs(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch blocked user IDs: %w", err)
		}

		if len(allBlockedIDs) > 0 {
			if a.db != nil && !a.opts.DryRun {
				if err := a.db.AddBlockedUsers(ctx, allBlockedIDs); err != nil {
					return fmt.Errorf("failed to add blocked users to database: %w", err)
				}
			}
			logging.Infof("Saved %d blocked IDs to database.", len(allBlockedIDs))
		} else {
			if !a.opts.Refresh {
				logging.Info("No blocked IDs found from the API.")
				return nil
			}
		}
	}

	var pendingIDs []int64
	if a.opts.DryRun && (totalBlockedCount == 0 || a.opts.Refresh) {
		pendingIDs = allBlockedIDs
	} else if a.db != nil {
		pendingIDs, err = a.db.GetPendingBlockedUsers(ctx)
		if err != nil {
			return fmt.Errorf("failed to get pending blocked users: %w", err)
		}
	}

	logging.Infof("Remaining accounts to unblock: %d.", len(pendingIDs))
	if len(pendingIDs) == 0 {
		logging.Info("All accounts from the list have been unblocked. Nothing to do!")
		return nil
	}

	return a.unblockWorkerPool(ctx, pendingIDs)
}

type unblockResult struct {
	userID int64
	status string
	err    error
}

func (a *UnblockAgent) unblockWorkerPool(ctx context.Context, pendingIDs []int64) error {
	totalToUnblock := len(pendingIDs)
	logging.Infof("Starting the unblocking process for %d accounts...", totalToUnblock)

	startTime := time.Now()
	sessionStats := map[string]int{
		"UNBLOCKED": 0,
		"NOT_FOUND": 0,
		"FAILED":    0,
	}

	for i := 0; i < len(pendingIDs); i += a.batchSize {
		select {
		case <-ctx.Done():
			logging.Warn("Unblock execution cancelled.")
			return ctx.Err()
		default:
		}

		end := i + a.batchSize
		if end > len(pendingIDs) {
			end = len(pendingIDs)
		}
		chunk := pendingIDs[i:end]

		chunkResults := a.processChunk(ctx, chunk)

		var unblockedIDs, notFoundIDs, failedIDs []int64
		for _, res := range chunkResults {
			switch res.status {
			case "UNBLOCKED", "SUCCESS":
				unblockedIDs = append(unblockedIDs, res.userID)
				sessionStats["UNBLOCKED"]++
			case "NOT_FOUND":
				notFoundIDs = append(notFoundIDs, res.userID)
				sessionStats["NOT_FOUND"]++
			default:
				failedIDs = append(failedIDs, res.userID)
				sessionStats["FAILED"]++
			}
		}

		if a.db != nil && !a.opts.DryRun {
			if len(unblockedIDs) > 0 {
				_ = a.db.UpdateBlockedUserStatuses(ctx, unblockedIDs, "UNBLOCKED")
			}
			if len(notFoundIDs) > 0 {
				_ = a.db.UpdateBlockedUserStatuses(ctx, notFoundIDs, "NOT_FOUND")
			}
			if len(failedIDs) > 0 {
				_ = a.db.UpdateBlockedUserStatuses(ctx, failedIDs, "FAILED")
			}
		}
	}

	duration := time.Since(startTime)
	totalProcessed := sessionStats["UNBLOCKED"] + sessionStats["NOT_FOUND"] + sessionStats["FAILED"]
	rate := 0.0
	if duration.Seconds() > 0 {
		rate = float64(totalProcessed) / duration.Seconds()
	}

	logging.Info("\n--- Unblocking Process Complete! ---")
	logging.Infof("Time taken: %.2f seconds", duration.Seconds())
	logging.Infof("Rate: %.2f accounts/second", rate)
	logging.Infof("Total accounts unblocked in this session: %d", sessionStats["UNBLOCKED"])
	if sessionStats["NOT_FOUND"] > 0 {
		logging.Infof("Accounts not found (deleted/suspended): %d", sessionStats["NOT_FOUND"])
	}
	if sessionStats["FAILED"] > 0 {
		logging.Warnf("Failed to unblock %d accounts. They will be retried on the next run.", sessionStats["FAILED"])
	}

	return nil
}

func (a *UnblockAgent) processChunk(ctx context.Context, chunk []int64) []unblockResult {
	results := make([]unblockResult, len(chunk))
	sem := make(chan struct{}, a.workers)
	var wg sync.WaitGroup

	for idx, uid := range chunk {
		wg.Add(1)
		go func(index int, userID int64) {
			defer wg.Done()

			if a.opts.DryRun {
				logging.SingleLinef("[Dry Run] Would unblock %d", userID)
				results[index] = unblockResult{userID: userID, status: "UNBLOCKED"}
				return
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index] = unblockResult{userID: userID, status: "FAILED", err: ctx.Err()}
				return
			}

			status, err := a.client.UnblockUser(ctx, userID)
			logging.SingleLinef("Processed ID %d: %s", userID, status)
			results[index] = unblockResult{userID: userID, status: status, err: err}
		}(idx, uid)
	}

	wg.Wait()
	return results
}
