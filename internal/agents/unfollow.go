package agents

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// UnfollowDB defines the SQLite operations required by UnfollowAgent.
type UnfollowDB interface {
	GetFollowerIDs(ctx context.Context) ([]int64, error)
	ReplaceFollowers(ctx context.Context, followerIDs []int64) error
	LogUnfollows(ctx context.Context, userIDs []int64) error
}

// UnfollowOptions holds configuration options for UnfollowAgent.
type UnfollowOptions struct {
	DryRun bool
	Email  bool
}

// UnfollowAgent detects follower churn and logs unfollow events.
type UnfollowAgent struct {
	client      xapi.XClient
	db          UnfollowDB
	cfg         *config.Config
	opts        UnfollowOptions
	out         io.Writer
	report      string
	emailSender EmailSender
}

// NewUnfollowAgent constructs a new UnfollowAgent instance.
func NewUnfollowAgent(client xapi.XClient, dbMgr UnfollowDB, cfg *config.Config, opts UnfollowOptions, out io.Writer) *UnfollowAgent {
	if out == nil {
		out = os.Stdout
	}
	return &UnfollowAgent{
		client:      client,
		db:          dbMgr,
		cfg:         cfg,
		opts:        opts,
		out:         out,
		emailSender: SendReportEmail,
	}
}

// SetEmailSender overrides the email sender for testing.
func (a *UnfollowAgent) SetEmailSender(sender EmailSender) {
	a.emailSender = sender
}

// Report returns the generated text report.
func (a *UnfollowAgent) Report() string {
	return a.report
}

// Run executes the complete unfollow detection workflow.
func (a *UnfollowAgent) Run(ctx context.Context) error {
	logging.Info("--- X Unfollow Detection Agent ---")

	// 1) Get current follower list from the API
	logging.Info("Fetching current followers from X API...")
	currentFollowers, err := a.client.GetFollowerIDs(ctx)
	if err != nil {
		return fmt.Errorf("fetch follower IDs: %w", err)
	}

	// 2) Compare to the follower list stored in the DB
	logging.Info("Comparing with previously stored followers...")
	previousFollowers, err := a.db.GetFollowerIDs(ctx)
	if err != nil {
		return fmt.Errorf("query previous followers from db: %w", err)
	}

	currentSet := make(map[int64]bool, len(currentFollowers))
	for _, id := range currentFollowers {
		currentSet[id] = true
	}

	var unfollowedIDs []int64
	var newFollowersCount int

	if len(previousFollowers) == 0 {
		logging.Info("No previous follower data found. This is likely the first run.")
		newFollowersCount = len(currentFollowers)
	} else {
		prevSet := make(map[int64]bool, len(previousFollowers))
		for _, id := range previousFollowers {
			prevSet[id] = true
		}

		for id := range prevSet {
			if !currentSet[id] {
				unfollowedIDs = append(unfollowedIDs, id)
			}
		}

		for id := range currentSet {
			if !prevSet[id] {
				newFollowersCount++
			}
		}
	}

	// 3) Store the new follower list
	if a.opts.DryRun {
		logging.Info("[Dry Run] Would update follower list in database.")
	} else {
		logging.Info("Updating follower list in database...")
		if err := a.db.ReplaceFollowers(ctx, currentFollowers); err != nil {
			return fmt.Errorf("update followers table: %w", err)
		}
	}

	// 4) Resolve user handles and generate report
	report, err := a.reportStats(ctx, len(currentFollowers), unfollowedIDs, newFollowersCount)
	if err != nil {
		return fmt.Errorf("report stats: %w", err)
	}
	a.report = report
	fmt.Fprintln(a.out, report)

	// 5) Log unfollow events
	if len(unfollowedIDs) > 0 {
		if a.opts.DryRun {
			logging.Infof("[Dry Run] Would log %d unfollow events.", len(unfollowedIDs))
		} else {
			logging.Infof("Logging %d unfollow events...", len(unfollowedIDs))
			if err := a.db.LogUnfollows(ctx, unfollowedIDs); err != nil {
				return fmt.Errorf("log unfollows to db: %w", err)
			}
		}
	}

	// 6) Email delivery if requested
	if a.opts.Email && a.cfg != nil {
		subject := fmt.Sprintf("X Unfollow Detection Report - %s", strings.ToUpper(a.cfg.Environment))
		if err := a.emailSender(ctx, a.cfg, subject, report); err != nil {
			logging.Errorf("Failed to deliver unfollow report email: %v", err)
			return err
		}
	}

	logging.Info("Unfollow detection completed.")
	return nil
}

func (a *UnfollowAgent) reportStats(ctx context.Context, currentTotal int, unfollowedIDs []int64, newFollowersCount int) (string, error) {
	var lines []string
	lines = append(lines, "\n--- Unfollow Detection Report ---")
	lines = append(lines, fmt.Sprintf("Total Followers: %d", currentTotal))
	lines = append(lines, fmt.Sprintf("New Followers:   %d", newFollowersCount))
	lines = append(lines, fmt.Sprintf("Unfollows:       %d", len(unfollowedIDs)))

	if len(unfollowedIDs) > 0 {
		lines = append(lines, "\nAccounts that unfollowed you:")

		userMap := make(map[int64]string)
		users, err := a.client.GetUsersBatch(ctx, unfollowedIDs)
		if err != nil {
			logging.Warnf("Warning: could not batch lookup unfollowed users: %v", err)
		} else {
			for id, u := range users {
				userMap[id] = u.Username
			}
		}

		sortedIDs := make([]int64, len(unfollowedIDs))
		copy(sortedIDs, unfollowedIDs)
		sort.Slice(sortedIDs, func(i, j int) bool { return sortedIDs[i] < sortedIDs[j] })

		for _, uid := range sortedIDs {
			handle := userMap[uid]
			if handle != "" {
				if strings.HasPrefix(handle, "(") {
					lines = append(lines, fmt.Sprintf(" - ID: %d %s", uid, handle))
				} else {
					lines = append(lines, fmt.Sprintf(" - @%s (ID: %d)", handle, uid))
				}
			} else {
				lines = append(lines, fmt.Sprintf(" - %d", uid))
			}
		}
	}

	lines = append(lines, "---------------------------------\n")
	return strings.Join(lines, "\n"), nil
}
