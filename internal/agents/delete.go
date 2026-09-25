package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// Deletion rule thresholds matching Python reference implementation.
const (
	CriticalAgeDays       = 365
	GracePeriodDays       = 7
	RetweetMaxAgeDays     = 30
	PopularTweetThreshold = 20
	PopularReplyThreshold = 5
	LinkTweetThreshold    = 10
	LinkReplyThreshold    = 3
)

// DeleteDB defines SQLite operations required by DeleteAgent.
type DeleteDB interface {
	GetDeletedTweetIDSet(ctx context.Context) (map[int64]bool, error)
	LogDeletedTweet(ctx context.Context, tweetID int64, text string, createdAt string, engagementScore int, isResponse bool) error
}

// DeleteOptions parameterizes the DeleteAgent run.
type DeleteOptions struct {
	ArchivePath  string
	ProtectedIDs []int64
	DryRun       bool
	Email        bool
	Days         int
	MinViews     int
	KeepPinned   bool
	MaxDelete    int
}

// DeleteStats tracks deletion metrics during execution.
type DeleteStats struct {
	Deleted int
	Skipped int
	Errors  int
}

// DeleteAgent prunes old tweets from Twitter archives or live timelines.
type DeleteAgent struct {
	xClient      xapi.XClient
	db           DeleteDB
	cfg          *config.Config
	opts         DeleteOptions
	stats        DeleteStats
	statsMu      sync.Mutex
	protectedIDs map[int64]bool
	deletedIDs   map[int64]bool
	report       string
	nowFn        func() time.Time
	sleepFn      func(time.Duration)
	emailSender  EmailSender
}

// NewDeleteAgent constructs a new DeleteAgent instance.
func NewDeleteAgent(client xapi.XClient, dbMgr DeleteDB, cfg *config.Config, opts DeleteOptions) *DeleteAgent {
	if opts.Days <= 0 {
		opts.Days = 30
	}
	if opts.MinViews <= 0 {
		opts.MinViews = 100
	}

	prot := make(map[int64]bool, len(opts.ProtectedIDs))
	for _, id := range opts.ProtectedIDs {
		prot[id] = true
	}

	return &DeleteAgent{
		xClient:      client,
		db:           dbMgr,
		cfg:          cfg,
		opts:         opts,
		protectedIDs: prot,
		deletedIDs:   make(map[int64]bool),
		nowFn:        func() time.Time { return time.Now().UTC() },
		sleepFn:      time.Sleep,
		emailSender:  SendReportEmail,
	}
}

// SetTimeFunc overrides the clock for deterministic testing.
func (a *DeleteAgent) SetTimeFunc(fn func() time.Time) {
	a.nowFn = fn
}

// SetSleepFunc overrides time.Sleep for testing.
func (a *DeleteAgent) SetSleepFunc(fn func(time.Duration)) {
	a.sleepFn = fn
}

// SetEmailSender overrides the email sender for testing.
func (a *DeleteAgent) SetEmailSender(sender EmailSender) {
	a.emailSender = sender
}

// Report returns the generated deletion session summary.
func (a *DeleteAgent) Report() string {
	return a.report
}

// Stats returns a copy of current execution counters.
func (a *DeleteAgent) Stats() DeleteStats {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	return a.stats
}

// Run executes the complete tweet pruning pipeline.
func (a *DeleteAgent) Run(ctx context.Context) error {
	logging.Info("--- X Delete Agent ---")

	// Pre-load already deleted IDs from SQLite to skip redundant processing
	if a.db != nil {
		deletedSet, err := a.db.GetDeletedTweetIDSet(ctx)
		if err != nil {
			logging.Warnf("Failed to load deleted tweet IDs from database: %v", err)
		} else {
			a.deletedIDs = deletedSet
		}
	}
	logging.Infof("Loaded %d already deleted tweet IDs.", len(a.deletedIDs))

	if a.opts.DryRun {
		logging.Info("DRY RUN ENABLED: No tweets will be actually deleted.")
	}

	now := a.nowFn()

	if a.opts.ArchivePath != "" {
		if err := a.processArchive(ctx, now); err != nil {
			logging.Errorf("Failed to process archive: %v", err)
		}
	} else {
		if err := a.processLive(ctx, now); err != nil {
			logging.Errorf("Failed to process live timeline: %v", err)
		}
	}

	a.report = a.generateReport()
	logging.Info(a.report)

	if a.opts.Email && a.cfg != nil && a.report != "" {
		subject := fmt.Sprintf("X Account Insights Report - %s", strings.ToUpper(a.cfg.Environment))
		if err := a.emailSender(ctx, a.cfg, subject, a.report); err != nil {
			logging.Errorf("Failed to deliver report email: %v", err)
			return err
		}
	}

	return nil
}

type flexibleID int64

func (f *flexibleID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return err
		}
		*f = flexibleID(id)
		return nil
	}
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*f = flexibleID(i)
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into flexibleID", string(data))
}

type flexibleInt int

func (f *flexibleInt) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		i, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return err
		}
		*f = flexibleInt(i)
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*f = flexibleInt(i)
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into flexibleInt", string(data))
}

type rawArchiveItem struct {
	Tweet *rawTweetData `json:"tweet"`
	rawTweetData
}

type rawTweetData struct {
	ID                flexibleID            `json:"id"`
	CreatedAt         string                `json:"created_at"`
	FullText          string                `json:"full_text"`
	Text              string                `json:"text"`
	FavoriteCount     flexibleInt           `json:"favorite_count"`
	RetweetCount      flexibleInt           `json:"retweet_count"`
	InReplyToStatusID *flexibleID           `json:"in_reply_to_status_id"`
	Entities          xapi.Entities         `json:"entities"`
	ExtendedEntities  xapi.ExtendedEntities `json:"extended_entities"`
}

var archiveDateFormats = []string{
	"Mon Jan 02 15:04:05 -0700 2006",
	time.RubyDate,
	time.RFC3339,
	time.RFC1123Z,
	"2006-01-02 15:04:05",
}

func parseArchiveDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range archiveDateFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown date layout: %q", s)
}

func (a *DeleteAgent) processArchive(ctx context.Context, now time.Time) error {
	content, err := os.ReadFile(a.opts.ArchivePath)
	if err != nil {
		logging.Errorf("Archive file not found: %s", a.opts.ArchivePath)
		return nil
	}

	logging.Infof("Processing archive: %s", a.opts.ArchivePath)

	contentStr := string(content)
	startIdx := strings.Index(contentStr, "[")
	if startIdx == -1 {
		logging.Errorf("Failed to process archive: malformed archive content (missing '[')")
		return nil
	}

	endIdx := strings.LastIndex(contentStr, "]")
	if endIdx == -1 || endIdx < startIdx {
		logging.Errorf("Failed to process archive: malformed archive content (missing ']')")
		return nil
	}

	jsonStr := contentStr[startIdx : endIdx+1]

	var items []rawArchiveItem
	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		logging.Errorf("Failed to process archive: %v", err)
		return nil
	}

	logging.Infof("Found %d tweets in archive.", len(items))

	if a.opts.KeepPinned && a.xClient != nil {
		if me, err := a.xClient.GetMe(ctx); err == nil && me != nil && me.PinnedTweetID != 0 {
			a.protectedIDs[me.PinnedTweetID] = true
			logging.Infof("Protected pinned tweet: %d", me.PinnedTweetID)
		}
	}

	for _, item := range items {
		data := item.rawTweetData
		if item.Tweet != nil {
			data = *item.Tweet
		}

		t, err := parseArchiveDate(data.CreatedAt)
		if err != nil {
			logging.Warnf("Skipping tweet %d with invalid date %q: %v", data.ID, data.CreatedAt, err)
			continue
		}

		txt := data.FullText
		if txt == "" {
			txt = data.Text
		}

		var inReplyID *int64
		if data.InReplyToStatusID != nil && *data.InReplyToStatusID != 0 {
			id := int64(*data.InReplyToStatusID)
			inReplyID = &id
		}

		tweet := &xapi.Tweet{
			ID:                int64(data.ID),
			IDStr:             strconv.FormatInt(int64(data.ID), 10),
			Text:              txt,
			FullText:          txt,
			CreatedAt:         t,
			FavoriteCount:     int(data.FavoriteCount),
			RetweetCount:      int(data.RetweetCount),
			InReplyToStatusID: inReplyID,
			Entities:          data.Entities,
			ExtendedEntities:  data.ExtendedEntities,
		}

		if err := a.processTweet(ctx, tweet, now); err != nil {
			return err
		}

		if a.opts.MaxDelete > 0 && a.stats.Deleted >= a.opts.MaxDelete {
			logging.Infof("Reached max-delete limit of %d tweets.", a.opts.MaxDelete)
			break
		}
	}

	return nil
}

func (a *DeleteAgent) processLive(ctx context.Context, now time.Time) error {
	logging.Info("Using live API hybrid fetch...")

	if a.xClient == nil {
		return fmt.Errorf("xClient is required for live timeline processing")
	}

	me, err := a.xClient.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authenticated user: %w", err)
	}

	if a.opts.KeepPinned && me.PinnedTweetID != 0 {
		a.protectedIDs[me.PinnedTweetID] = true
		logging.Infof("Protected pinned tweet: %d", me.PinnedTweetID)
	}

	var maxID int64
	for {
		tweets, err := a.xClient.GetUserTimeline(ctx, me.ID, 200, maxID)
		if err != nil {
			if xapi.IsUnauthorized(err) {
				logging.Warnf("Reached API access limit for fetching tweets: %v. Processing what was already fetched.", err)
				break
			}
			logging.Errorf("Error fetching tweets: %v", err)
			break
		}

		if len(tweets) == 0 {
			break
		}

		prevMaxID := maxID
		for _, tweet := range tweets {
			if maxID > 0 && tweet.ID == maxID {
				continue
			}

			if err := a.processTweet(ctx, tweet, now); err != nil {
				return err
			}

			maxID = tweet.ID

			if a.opts.MaxDelete > 0 && a.stats.Deleted >= a.opts.MaxDelete {
				logging.Infof("Reached max-delete limit of %d tweets.", a.opts.MaxDelete)
				return nil
			}
		}

		if maxID == prevMaxID {
			break
		}

		a.sleepFn(1 * time.Second)
	}

	return nil
}

func (a *DeleteAgent) processTweet(ctx context.Context, tweet *xapi.Tweet, now time.Time) error {
	tweetID := tweet.ID

	// Checkpoint check: skip already deleted tweets
	if a.deletedIDs[tweetID] {
		logging.Debugf("Skipping already deleted tweet: %d", tweetID)
		a.statsMu.Lock()
		a.stats.Deleted++
		a.statsMu.Unlock()
		return nil
	}

	likes := tweet.FavoriteCount
	retweets := tweet.RetweetCount
	engagementScore := likes + retweets

	text := tweet.FullText
	if text == "" {
		text = tweet.Text
	}
	isResponse := tweet.InReplyToStatusID != nil && *tweet.InReplyToStatusID != 0
	age := now.Sub(tweet.CreatedAt)

	hasMedia := len(tweet.Entities.Media) > 0 || len(tweet.ExtendedEntities.Media) > 0
	hasLink := len(tweet.Entities.URLs) > 0
	isThread := strings.Contains(text, "1/") || strings.Contains(text, "🧵")
	isRetweet := strings.HasPrefix(text, "RT @")

	// RULE 1: Protected IDs
	if a.protectedIDs[tweetID] {
		logging.Infof("KEEP [Protected] ID: %d", tweetID)
		a.statsMu.Lock()
		a.stats.Skipped++
		a.statsMu.Unlock()
		return nil
	}

	// RULE 2: Grace Period (< 7 days)
	if age < time.Duration(GracePeriodDays)*24*time.Hour {
		days := int(age.Hours() / 24)
		logging.Infof("KEEP [Recent]    ID: %d (Age: %dd)", tweetID, days)
		a.statsMu.Lock()
		a.stats.Skipped++
		a.statsMu.Unlock()
		return nil
	}

	// RULE 3: Retweet Cleanup (> 30 days)
	if isRetweet && age > time.Duration(RetweetMaxAgeDays)*24*time.Hour {
		reason := fmt.Sprintf("old retweet (> %d days)", RetweetMaxAgeDays)
		return a.deleteTweet(ctx, tweet, engagementScore, isResponse, reason)
	}

	// RULE 4: High Value Content (Threads & Media)
	if isThread {
		logging.Infof("KEEP [Thread]    ID: %d", tweetID)
		a.statsMu.Lock()
		a.stats.Skipped++
		a.statsMu.Unlock()
		return nil
	}

	if hasMedia {
		logging.Infof("KEEP [Media]     ID: %d", tweetID)
		a.statsMu.Lock()
		a.stats.Skipped++
		a.statsMu.Unlock()
		return nil
	}

	// RULE 5: Critical Age (> 1 year / 365 days)
	if age > time.Duration(CriticalAgeDays)*24*time.Hour {
		reason := fmt.Sprintf("older than %d days and no special protection", CriticalAgeDays)
		return a.deleteTweet(ctx, tweet, engagementScore, isResponse, reason)
	}

	// RULE 6: Engagement Thresholds
	var threshold int
	if isResponse {
		if hasLink {
			threshold = LinkReplyThreshold
		} else {
			threshold = PopularReplyThreshold
		}
	} else {
		if hasLink {
			threshold = LinkTweetThreshold
		} else {
			threshold = PopularTweetThreshold
		}
	}

	if a.opts.MinViews > 0 && tweet.PublicMetrics.ImpressionCount >= a.opts.MinViews {
		logging.Infof("KEEP [High-Views  ] ID: %d (%d views >= %d threshold)", tweetID, tweet.PublicMetrics.ImpressionCount, a.opts.MinViews)
		a.statsMu.Lock()
		a.stats.Skipped++
		a.statsMu.Unlock()
		return nil
	}

	if engagementScore >= threshold {
		kind := "Popular"
		if isResponse {
			kind = "Pop-Reply"
		}
		if hasLink {
			kind += "+Link"
		}
		logging.Infof("KEEP [%-12s] ID: %d (%d likes+rt, threshold: %d)", kind, tweetID, engagementScore, threshold)
		a.statsMu.Lock()
		a.stats.Skipped++
		a.statsMu.Unlock()
		return nil
	}

	// RULE 7: Low engagement fallback
	reason := fmt.Sprintf("low engagement (%d < %d)", engagementScore, threshold)
	return a.deleteTweet(ctx, tweet, engagementScore, isResponse, reason)
}

func (a *DeleteAgent) deleteTweet(ctx context.Context, tweet *xapi.Tweet, engagement int, isResponse bool, reason string) error {
	tweetID := tweet.ID
	text := tweet.FullText
	if text == "" {
		text = tweet.Text
	}
	displayText := strings.ReplaceAll(text, "\n", " ")
	if len(displayText) > 60 {
		displayText = displayText[:60]
	}

	if a.opts.DryRun {
		logging.Infof("DELETE           ID: %d | Eng: %d | %s...", tweetID, engagement, displayText)
		a.statsMu.Lock()
		a.stats.Deleted++
		a.statsMu.Unlock()
		return nil
	}

	logging.Infof("Deleting tweet %d: %s", tweetID, reason)
	if a.xClient == nil {
		a.statsMu.Lock()
		a.stats.Errors++
		a.statsMu.Unlock()
		return nil
	}

	success, err := a.xClient.DeleteTweet(ctx, tweetID)
	if err != nil {
		if xapi.IsNotFound(err) {
			success = true
		} else {
			logging.Errorf("Failed to delete tweet %d: %v", tweetID, err)
			a.statsMu.Lock()
			a.stats.Errors++
			a.statsMu.Unlock()
			return nil
		}
	}

	if success {
		a.statsMu.Lock()
		a.stats.Deleted++
		a.deletedIDs[tweetID] = true
		a.statsMu.Unlock()

		if a.db != nil {
			createdAtStr := tweet.CreatedAt.Format(time.RFC3339)
			if err := a.db.LogDeletedTweet(ctx, tweetID, text, createdAtStr, engagement, isResponse); err != nil {
				logging.Warnf("Failed to log deleted tweet %d to database: %v", tweetID, err)
			}
		}

		a.sleepFn(1 * time.Second)
	} else {
		a.statsMu.Lock()
		a.stats.Errors++
		a.statsMu.Unlock()
	}

	return nil
}

func (a *DeleteAgent) generateReport() string {
	total := a.stats.Deleted + a.stats.Skipped + a.stats.Errors
	return fmt.Sprintf("\n--- Delete Agent Report ---\nTweets Processed: %d\nTweets Deleted:   %d\nTweets Skipped:   %d\nErrors:           %d\n---------------------------\n",
		total, a.stats.Deleted, a.stats.Skipped, a.stats.Errors)
}
