package agents

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/db"
	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// InsightsDB defines SQLite operations required by InsightsAgent.
type InsightsDB interface {
	GetFollowerIDs(ctx context.Context) ([]int64, error)
	GetFollowerUsernames(ctx context.Context) (map[int64]string, error)
	ReplaceFollowers(ctx context.Context, followerIDs []int64) error
	UpsertFollowerUsernames(ctx context.Context, usernames map[int64]string) error
	GetLatestInsight(ctx context.Context) (*db.Insight, error)
	GetInsightAtOffset(ctx context.Context, offsetDays int) (*db.Insight, error)
	AddInsight(ctx context.Context, followers, following, tweetCount, listedCount int) error
}

// InsightsOptions holds configuration options for InsightsAgent.
type InsightsOptions struct {
	Email bool
}

// InsightsAgent gathers and reports comprehensive account metrics.
type InsightsAgent struct {
	client      xapi.XClient
	db          InsightsDB
	cfg         *config.Config
	opts        InsightsOptions
	out         io.Writer
	report      string
	nowFn       func() time.Time
	emailSender EmailSender
}

// NewInsightsAgent constructs a new InsightsAgent instance.
func NewInsightsAgent(client xapi.XClient, dbMgr InsightsDB, cfg *config.Config, opts InsightsOptions, out io.Writer) *InsightsAgent {
	if out == nil {
		out = os.Stdout
	}
	return &InsightsAgent{
		client:      client,
		db:          dbMgr,
		cfg:         cfg,
		opts:        opts,
		out:         out,
		nowFn:       func() time.Time { return time.Now().UTC() },
		emailSender: SendReportEmail,
	}
}

// SetNowFunc overrides the clock for deterministic testing.
func (a *InsightsAgent) SetNowFunc(fn func() time.Time) {
	a.nowFn = fn
}

// SetEmailSender overrides the email sender for testing.
func (a *InsightsAgent) SetEmailSender(sender EmailSender) {
	a.emailSender = sender
}

// Report returns the generated text report.
func (a *InsightsAgent) Report() string {
	return a.report
}

type insightsReportData struct {
	FollowersCount int
	FollowingCount int
	TweetCount     int
	ListedCount    int
	CreatedAt      time.Time
	Comparisons    map[string]*db.Insight
	NewUserMap     map[int64]string
	LostUserMap    map[int64]string
	CachedUserMap  map[int64]string
	NewIDs         []int64
	LostIDs        []int64
}

// Run executes the complete insights collection, persistence, and reporting workflow.
func (a *InsightsAgent) Run(ctx context.Context) error {
	logging.Info("Starting the insights agent...")

	me, err := a.client.GetMe(ctx)
	if err != nil {
		logging.Errorf("Could not retrieve user metrics: %v", err)
		return fmt.Errorf("retrieve user metrics: %w", err)
	}

	followersCount := me.FollowersCount
	followingCount := me.FollowingCount
	tweetCount := me.TweetCount
	listedCount := me.ListedCount
	createdAt := me.CreatedAt

	logging.Info("Fetching current follower IDs for change detection...")
	currentFollowerIDs, err := a.client.GetFollowerIDs(ctx)
	if err != nil {
		return fmt.Errorf("fetch follower IDs: %w", err)
	}

	previousFollowerIDs, err := a.db.GetFollowerIDs(ctx)
	if err != nil {
		return fmt.Errorf("query previous followers: %w", err)
	}

	cachedUsernames, err := a.db.GetFollowerUsernames(ctx)
	if err != nil {
		logging.Warnf("Warning: failed to load cached follower usernames: %v", err)
		cachedUsernames = make(map[int64]string)
	} else if cachedUsernames == nil {
		cachedUsernames = make(map[int64]string)
	}

	currentSet := make(map[int64]bool, len(currentFollowerIDs))
	for _, id := range currentFollowerIDs {
		currentSet[id] = true
	}
	prevSet := make(map[int64]bool, len(previousFollowerIDs))
	for _, id := range previousFollowerIDs {
		prevSet[id] = true
	}

	var newIDs, lostIDs []int64
	newSet := make(map[int64]bool)
	newUserMap := make(map[int64]string)
	lostUserMap := make(map[int64]string)
	backfillMap := make(map[int64]string)

	if len(previousFollowerIDs) > 0 {
		for id := range currentSet {
			if !prevSet[id] {
				newIDs = append(newIDs, id)
				newSet[id] = true
			}
		}
		for id := range prevSet {
			if !currentSet[id] {
				lostIDs = append(lostIDs, id)
			}
		}
	}

	const maxBackfillPerRun = 100
	var backfillIDs []int64
	seenBackfill := make(map[int64]bool)
	for _, id := range currentFollowerIDs {
		if len(backfillIDs) >= maxBackfillPerRun {
			break
		}
		if newSet[id] || seenBackfill[id] {
			continue
		}
		if cachedUsernames[id] == "" {
			seenBackfill[id] = true
			backfillIDs = append(backfillIDs, id)
		}
	}

	var wg sync.WaitGroup
	var newErr, lostErr, backfillErr error

	if len(previousFollowerIDs) > 0 {
		if len(newIDs) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				newUserMap, newErr = a.resolveUserMap(ctx, newIDs, "new")
			}()
		}
		if len(lostIDs) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				lostUserMap, lostErr = a.resolveUserMap(ctx, lostIDs, "lost")
			}()
		}
	}
	if len(backfillIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			backfillMap, backfillErr = a.resolveUserMap(ctx, backfillIDs, "existing")
		}()
	}
	wg.Wait()

	if newErr != nil {
		logging.Warnf("Warning: error resolving new follower names: %v", newErr)
	}
	if lostErr != nil {
		logging.Warnf("Warning: error resolving lost follower names: %v", lostErr)
	}
	if backfillErr != nil {
		logging.Warnf("Warning: error resolving existing follower names: %v", backfillErr)
	}

	if err := a.db.ReplaceFollowers(ctx, currentFollowerIDs); err != nil {
		return fmt.Errorf("update followers table: %w", err)
	}

	toCache := make(map[int64]string)
	for id, raw := range newUserMap {
		h := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
		if h != "" && !strings.HasPrefix(strings.TrimSpace(raw), "(") && !strings.HasPrefix(h, "(") {
			toCache[id] = h
		}
	}
	for id, raw := range backfillMap {
		h := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
		if h != "" && !strings.HasPrefix(strings.TrimSpace(raw), "(") && !strings.HasPrefix(h, "(") {
			toCache[id] = h
		}
	}
	if len(toCache) > 0 {
		if err := a.db.UpsertFollowerUsernames(ctx, toCache); err != nil {
			logging.Warnf("Warning: failed to cache follower usernames: %v", err)
		}
	}

	comparisons := make(map[string]*db.Insight)
	if prev, err := a.db.GetLatestInsight(ctx); err == nil && prev != nil {
		comparisons["Previous"] = prev
	}
	if h24, err := a.db.GetInsightAtOffset(ctx, 1); err == nil && h24 != nil {
		comparisons["24h Ago"] = h24
	}
	if d7, err := a.db.GetInsightAtOffset(ctx, 7); err == nil && d7 != nil {
		comparisons["7d Ago"] = d7
	}
	if d30, err := a.db.GetInsightAtOffset(ctx, 30); err == nil && d30 != nil {
		comparisons["30d Ago"] = d30
	}

	data := insightsReportData{
		FollowersCount: followersCount,
		FollowingCount: followingCount,
		TweetCount:     tweetCount,
		ListedCount:    listedCount,
		CreatedAt:      createdAt,
		Comparisons:    comparisons,
		NewUserMap:     newUserMap,
		LostUserMap:    lostUserMap,
		CachedUserMap:  cachedUsernames,
		NewIDs:         newIDs,
		LostIDs:        lostIDs,
	}

	a.report = a.generateReport(data)
	fmt.Fprintln(a.out, a.report)

	if err := a.db.AddInsight(ctx, followersCount, followingCount, tweetCount, listedCount); err != nil {
		return fmt.Errorf("save insight to db: %w", err)
	}

	if a.opts.Email && a.cfg != nil {
		subject := fmt.Sprintf("X Account Insights Report - %s", strings.ToUpper(a.cfg.Environment))
		if err := a.emailSender(ctx, a.cfg, subject, a.report); err != nil {
			logging.Errorf("Failed to deliver report email: %v", err)
			return err
		}
	}

	logging.Info("Insights agent finished successfully.")
	return nil
}

func (a *InsightsAgent) resolveUserMap(ctx context.Context, ids []int64, label string) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	logging.Infof("Resolving %d %s follower usernames...", len(ids), label)
	batch, err := a.client.GetUsersBatch(ctx, ids)
	if err != nil {
		return nil, err
	}
	res := make(map[int64]string, len(ids))
	for _, id := range ids {
		if u, ok := batch[id]; ok && u != nil && u.Username != "" {
			res[id] = u.Username
		}
	}
	return res, nil
}

func (a *InsightsAgent) generateReport(d insightsReportData) string {
	const width = 42
	var lines []string

	lines = append(lines, "\n"+strings.Repeat("=", width))
	lines = append(lines, "      🚀 X ACCOUNT MASTER INSIGHTS 🚀")
	lines = append(lines, strings.Repeat("=", width))

	// 1. Core Metrics
	ratio := 0.0
	if d.FollowingCount > 0 {
		ratio = float64(d.FollowersCount) / float64(d.FollowingCount)
	}
	lines = append(lines, fmt.Sprintf("Followers: %s", formatCommas(d.FollowersCount)))
	lines = append(lines, fmt.Sprintf("Following: %s", formatCommas(d.FollowingCount)))
	lines = append(lines, fmt.Sprintf("Tweets:    %s", formatCommas(d.TweetCount)))
	lines = append(lines, fmt.Sprintf("Listed:    %s", formatCommas(d.ListedCount)))
	lines = append(lines, fmt.Sprintf("Ratio:     %.2f", ratio))
	lines = append(lines, strings.Repeat("-", width))

	// 2. Follower Changes
	newIDs := dedupInt64Slice(d.NewIDs)
	lostIDs := dedupInt64Slice(d.LostIDs)
	if len(newIDs) > 0 || len(lostIDs) > 0 {
		lines = append(lines, "           FOLLOWERS LOG")
		if len(newIDs) > 0 {
			lines = append(lines, fmt.Sprintf("New (%d):", len(newIDs)))
			lines = append(lines, formatFollowerSection("+", newIDs, d.NewUserMap, d.CachedUserMap)...)
		}
		if len(lostIDs) > 0 {
			lines = append(lines, fmt.Sprintf("Lost (%d):", len(lostIDs)))
			lines = append(lines, formatFollowerSection("-", lostIDs, d.LostUserMap, d.CachedUserMap)...)
		}
		lines = append(lines, strings.Repeat("-", width))
	}

	// 3. Account Vitality
	now := a.nowFn().UTC()
	if !d.CreatedAt.IsZero() {
		creationDt := d.CreatedAt.UTC()
		diffDays := int(now.Sub(creationDt).Hours() / 24.0)
		ageDays := diffDays
		if ageDays < 1 {
			ageDays = 1
		}
		avgTweetsPerDay := float64(d.TweetCount) / float64(ageDays)

		lines = append(lines, "          ACCOUNT VITALITY")
		lines = append(lines, fmt.Sprintf("Age:      %s days", formatCommas(ageDays)))
		lines = append(lines, fmt.Sprintf("Activity: %.2f tweets/day", avgTweetsPerDay))
		lines = append(lines, strings.Repeat("-", width))
	}

	// 4. Historical Comparisons
	lines = append(lines, fmt.Sprintf("%-9s | %7s | %6s | %s", "Period", "Follows", "Tweets", "List"))
	lines = append(lines, strings.Repeat("-", width))

	hasHistory := false
	orderedLabels := []string{"Previous", "24h Ago", "7d Ago", "30d Ago"}
	for _, label := range orderedLabels {
		insight, ok := d.Comparisons[label]
		if !ok || insight == nil {
			continue
		}
		hasHistory = true

		fDelta := d.FollowersCount - insight.Followers
		tDelta := d.TweetCount - insight.TweetCount
		lDelta := d.ListedCount - insight.ListedCount

		fDeltaStr := fmt.Sprintf("%+d", fDelta)
		tDeltaStr := fmt.Sprintf("%+d", tDelta)
		lDeltaStr := fmt.Sprintf("%+d", lDelta)

		lines = append(lines, fmt.Sprintf("%-9s | %7s | %6s | %s", label, fDeltaStr, tDeltaStr, lDeltaStr))
	}

	if !hasHistory {
		lines = append(lines, "No historical data recorded yet.")
	}
	lines = append(lines, strings.Repeat("-", width))

	// 5. Growth Velocity & Projections
	dayInsight := d.Comparisons["24h Ago"]
	if dayInsight == nil {
		dayInsight = d.Comparisons["Previous"]
	}

	if dayInsight != nil {
		deltaSeconds := now.Sub(dayInsight.Timestamp).Seconds()
		deltaDays := deltaSeconds / 86400.0
		if deltaDays <= 0 {
			deltaDays = 1.0
		}

		dailyVelocity := float64(d.FollowersCount-dayInsight.Followers) / deltaDays

		if dailyVelocity > 0 {
			lines = append(lines, fmt.Sprintf("Velocity:  %.1f followers/day", dailyVelocity))
			milestones := []int{100, 500, 1000, 5000, 10000, 50000, 100000}
			for _, milestone := range milestones {
				if d.FollowersCount < milestone {
					daysToGo := int(float64(milestone-d.FollowersCount) / dailyVelocity)
					lines = append(lines, fmt.Sprintf("Target:    %s in %dd", formatCommas(milestone), daysToGo))
					break
				}
			}
		} else if dailyVelocity < 0 {
			lines = append(lines, fmt.Sprintf("Velocity:  %.1f (Downwards)", dailyVelocity))
		}
	}

	lines = append(lines, strings.Repeat("=", width)+"\n")
	return strings.Join(lines, "\n")
}

type namedFollowerLine struct {
	sortKey string
	line    string
}

func dedupInt64Slice(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func formatFollowerSection(prefix string, ids []int64, liveMap, cachedMap map[int64]string) []string {
	seen := make(map[int64]bool, len(ids))
	var named []namedFollowerLine
	var deletedCount, suspendedCount, unresolvedCount int

	for _, uid := range ids {
		if seen[uid] {
			continue
		}
		seen[uid] = true

		liveRaw := strings.TrimSpace(liveMap[uid])
		liveClean := strings.TrimSpace(strings.TrimPrefix(liveRaw, "@"))
		cachedClean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cachedMap[uid]), "@"))
		if strings.HasPrefix(cachedClean, "(") {
			cachedClean = ""
		}

		switch {
		case liveClean != "" && !strings.HasPrefix(liveRaw, "(") && !strings.HasPrefix(liveClean, "("):
			named = append(named, namedFollowerLine{
				sortKey: strings.ToLower(liveClean),
				line:    fmt.Sprintf(" %s @%s", prefix, liveClean),
			})
		case liveRaw == "(Deactivated)":
			if cachedClean != "" {
				named = append(named, namedFollowerLine{
					sortKey: strings.ToLower(cachedClean),
					line:    fmt.Sprintf(" %s @%s (deleted)", prefix, cachedClean),
				})
			} else {
				deletedCount++
			}
		case liveRaw == "(Suspended)":
			if cachedClean != "" {
				named = append(named, namedFollowerLine{
					sortKey: strings.ToLower(cachedClean),
					line:    fmt.Sprintf(" %s @%s (suspended)", prefix, cachedClean),
				})
			} else {
				suspendedCount++
			}
		default:
			if cachedClean != "" {
				named = append(named, namedFollowerLine{
					sortKey: strings.ToLower(cachedClean),
					line:    fmt.Sprintf(" %s @%s", prefix, cachedClean),
				})
			} else {
				unresolvedCount++
			}
		}
	}

	sort.SliceStable(named, func(i, j int) bool {
		if named[i].sortKey != named[j].sortKey {
			return named[i].sortKey < named[j].sortKey
		}
		return named[i].line < named[j].line
	})

	out := make([]string, 0, len(named)+3)
	for _, item := range named {
		out = append(out, item.line)
	}

	if deletedCount == 1 {
		out = append(out, fmt.Sprintf(" %s 1 deleted account", prefix))
	} else if deletedCount > 1 {
		out = append(out, fmt.Sprintf(" %s %d deleted accounts", prefix, deletedCount))
	}

	if suspendedCount == 1 {
		out = append(out, fmt.Sprintf(" %s 1 suspended account", prefix))
	} else if suspendedCount > 1 {
		out = append(out, fmt.Sprintf(" %s %d suspended accounts", prefix, suspendedCount))
	}

	if unresolvedCount == 1 {
		out = append(out, fmt.Sprintf(" %s 1 unresolved account", prefix))
	} else if unresolvedCount > 1 {
		out = append(out, fmt.Sprintf(" %s %d unresolved accounts", prefix, unresolvedCount))
	}

	return out
}

func formatCommas[T int | int64](n T) string {
	in := strconv.FormatInt(int64(n), 10)
	var sign string
	if in != "" && in[0] == '-' {
		sign = "-"
		in = in[1:]
	}
	rem := len(in) % 3
	if rem == 0 {
		rem = 3
	}
	var buf strings.Builder
	buf.WriteString(sign)
	buf.WriteString(in[:rem])
	for i := rem; i < len(in); i += 3 {
		buf.WriteByte(',')
		buf.WriteString(in[i : i+3])
	}
	return buf.String()
}
