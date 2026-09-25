package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ----------------------------------------------------------------------------
// Strongly-Typed Domain Models
// ----------------------------------------------------------------------------

// Insight stores periodic snapshots of account statistics.
type Insight struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Followers   int       `json:"followers"`
	Following   int       `json:"following"`
	TweetCount  int       `json:"tweet_count"`
	ListedCount int       `json:"listed_count"`
}

// BlockedUser tracks blocked accounts and their unblock processing state.
type BlockedUser struct {
	UserID    int64     `json:"user_id"`
	Status    string    `json:"status"` // PENDING, FAILED, UNBLOCKED, PROCESSED
	UpdatedAt time.Time `json:"updated_at"`
}

// FollowingUser tracks followed accounts and their unfollow queue state.
type FollowingUser struct {
	UserID    int64     `json:"user_id"`
	Status    string    `json:"status"` // PENDING, FAILED, PROCESSED
	UpdatedAt time.Time `json:"updated_at"`
}

// Follower holds a point-in-time snapshot of accounts following the user.
type Follower struct {
	UserID    int64     `json:"user_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Unfollow provides an append-only audit trail of unfollow events.
type Unfollow struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Timestamp time.Time `json:"timestamp"`
}

// DeletedTweet records tweets pruned by DeleteAgent for auditing.
type DeletedTweet struct {
	TweetID         int64     `json:"tweet_id"`
	Text            string    `json:"text"`
	CreatedAt       string    `json:"created_at"`
	EngagementScore int       `json:"engagement_score"`
	IsResponse      bool      `json:"is_response"`
	DeletedAt       time.Time `json:"deleted_at"`
}

// SchemaVersion tracks applied migrations.
type SchemaVersion struct {
	Version     int       `json:"version"`
	AppliedAt   time.Time `json:"applied_at"`
	Description string    `json:"description"`
}

// ----------------------------------------------------------------------------
// Datetime Parsing Helper
// ----------------------------------------------------------------------------

var sqliteTimeFormats = []string{
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05.999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
}

func parseSQLiteTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range sqliteTimeFormats {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse SQLite datetime: %q", s)
}

// ----------------------------------------------------------------------------
// Insights Operations
// ----------------------------------------------------------------------------

// AddInsight inserts a new statistical snapshot into the insights table.
func (m *DBManager) AddInsight(ctx context.Context, followers, following, tweetCount, listedCount int) error {
	query := `INSERT INTO insights (followers, following, tweet_count, listed_count) VALUES (?, ?, ?, ?)`
	_, err := m.sqlDB.ExecContext(ctx, query, followers, following, tweetCount, listedCount)
	if err != nil {
		return fmt.Errorf("add insight: %w", err)
	}
	return nil
}

// GetLatestInsight retrieves the most recent insight record.
func (m *DBManager) GetLatestInsight(ctx context.Context) (*Insight, error) {
	query := `
		SELECT id, timestamp, COALESCE(followers, 0), COALESCE(following, 0), COALESCE(tweet_count, 0), COALESCE(listed_count, 0)
		FROM insights
		ORDER BY timestamp DESC, id DESC
		LIMIT 1`
	var i Insight
	var ts sql.NullString
	err := m.sqlDB.QueryRowContext(ctx, query).Scan(&i.ID, &ts, &i.Followers, &i.Following, &i.TweetCount, &i.ListedCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest insight: %w", err)
	}
	if ts.Valid {
		i.Timestamp, _ = parseSQLiteTime(ts.String)
	}
	return &i, nil
}

// GetInsightAtOffset returns the insight closest to daysAgo in the past.
func (m *DBManager) GetInsightAtOffset(ctx context.Context, daysAgo int) (*Insight, error) {
	query := `
		SELECT id, timestamp, COALESCE(followers, 0), COALESCE(following, 0), COALESCE(tweet_count, 0), COALESCE(listed_count, 0)
		FROM insights
		WHERE timestamp <= STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', ?)
		ORDER BY timestamp DESC, id DESC
		LIMIT 1`
	offsetParam := fmt.Sprintf("-%d days", daysAgo)
	var i Insight
	var ts sql.NullString
	err := m.sqlDB.QueryRowContext(ctx, query, offsetParam).Scan(&i.ID, &ts, &i.Followers, &i.Following, &i.TweetCount, &i.ListedCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get insight at offset (%d days): %w", daysAgo, err)
	}
	if ts.Valid {
		i.Timestamp, _ = parseSQLiteTime(ts.String)
	}
	return &i, nil
}

// ----------------------------------------------------------------------------
// Blocked Users Operations
// ----------------------------------------------------------------------------

// UpsertBlockedUsers adds or updates blocked users, resetting status to PENDING on conflict.
func (m *DBManager) UpsertBlockedUsers(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	return m.WithTx(ctx, func(tx *sql.Tx) error {
		query := `
			INSERT INTO blocked_users (user_id, status) VALUES (?, 'PENDING')
			ON CONFLICT(user_id) DO UPDATE SET status = 'PENDING'`
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return fmt.Errorf("prepare upsert blocked users: %w", err)
		}
		defer stmt.Close()

		for _, id := range userIDs {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return fmt.Errorf("exec upsert blocked user %d: %w", id, err)
			}
		}
		return nil
	})
}

// AddBlockedUsers is an alias for UpsertBlockedUsers for compatibility.
func (m *DBManager) AddBlockedUsers(ctx context.Context, userIDs []int64) error {
	return m.UpsertBlockedUsers(ctx, userIDs)
}

// GetPendingBlockedUsers returns all user IDs with status PENDING or FAILED.
func (m *DBManager) GetPendingBlockedUsers(ctx context.Context) ([]int64, error) {
	query := `SELECT user_id FROM blocked_users WHERE status IN ('PENDING', 'FAILED') ORDER BY user_id ASC`
	rows, err := m.sqlDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get pending blocked users: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan blocked user id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAllBlockedUsersCount returns total count of blocked users.
func (m *DBManager) GetAllBlockedUsersCount(ctx context.Context) (int, error) {
	var count int
	err := m.sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocked_users").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get all blocked users count: %w", err)
	}
	return count, nil
}

// ClearPendingBlockedUsers removes all records with status PENDING.
func (m *DBManager) ClearPendingBlockedUsers(ctx context.Context) error {
	_, err := m.sqlDB.ExecContext(ctx, "DELETE FROM blocked_users WHERE status = 'PENDING'")
	if err != nil {
		return fmt.Errorf("clear pending blocked users: %w", err)
	}
	return nil
}

// UpdateBlockedUserStatus updates a single user's status and updated_at timestamp.
func (m *DBManager) UpdateBlockedUserStatus(ctx context.Context, userID int64, status string) error {
	query := `UPDATE blocked_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?`
	_, err := m.sqlDB.ExecContext(ctx, query, status, userID)
	if err != nil {
		return fmt.Errorf("update blocked user %d status: %w", userID, err)
	}
	return nil
}

// UpdateUserStatus is an alias for UpdateBlockedUserStatus for parity with Python update_user_status.
func (m *DBManager) UpdateUserStatus(ctx context.Context, userID int64, status string) error {
	return m.UpdateBlockedUserStatus(ctx, userID, status)
}

// UpdateBlockedUserStatuses batch updates status for multiple users within a transaction.
func (m *DBManager) UpdateBlockedUserStatuses(ctx context.Context, userIDs []int64, status string) error {
	if len(userIDs) == 0 {
		return nil
	}
	return m.WithTx(ctx, func(tx *sql.Tx) error {
		query := `UPDATE blocked_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?`
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return fmt.Errorf("prepare update blocked user statuses: %w", err)
		}
		defer stmt.Close()

		for _, id := range userIDs {
			if _, err := stmt.ExecContext(ctx, status, id); err != nil {
				return fmt.Errorf("exec update user %d: %w", id, err)
			}
		}
		return nil
	})
}

// UpdateUserStatuses is an alias for UpdateBlockedUserStatuses for parity with Python update_user_statuses.
func (m *DBManager) UpdateUserStatuses(ctx context.Context, userIDs []int64, status string) error {
	return m.UpdateBlockedUserStatuses(ctx, userIDs, status)
}

// ----------------------------------------------------------------------------
// Following Users Operations
// ----------------------------------------------------------------------------

// SyncFollowing adds new followed users with status PENDING (ignoring existing).
func (m *DBManager) SyncFollowing(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	return m.WithTx(ctx, func(tx *sql.Tx) error {
		query := `INSERT OR IGNORE INTO following_users (user_id, status) VALUES (?, 'PENDING')`
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return fmt.Errorf("prepare sync following: %w", err)
		}
		defer stmt.Close()

		for _, id := range userIDs {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return fmt.Errorf("exec insert following %d: %w", id, err)
			}
		}
		return nil
	})
}

// AddFollowingUsers is an alias for SyncFollowing.
func (m *DBManager) AddFollowingUsers(ctx context.Context, userIDs []int64) error {
	return m.SyncFollowing(ctx, userIDs)
}

// GetFollowingIDs retrieves all user IDs from following_users.
func (m *DBManager) GetFollowingIDs(ctx context.Context) ([]int64, error) {
	rows, err := m.sqlDB.QueryContext(ctx, "SELECT user_id FROM following_users ORDER BY user_id ASC")
	if err != nil {
		return nil, fmt.Errorf("get following ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan following id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetPendingFollowingUsers retrieves all following user IDs with status PENDING or FAILED.
func (m *DBManager) GetPendingFollowingUsers(ctx context.Context) ([]int64, error) {
	query := `SELECT user_id FROM following_users WHERE status IN ('PENDING', 'FAILED') ORDER BY user_id ASC`
	rows, err := m.sqlDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get pending following users: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending following id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAllFollowingUsersCount returns total records in following_users.
func (m *DBManager) GetAllFollowingUsersCount(ctx context.Context) (int, error) {
	var count int
	err := m.sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM following_users").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get following users count: %w", err)
	}
	return count, nil
}

// GetProcessedFollowingCount returns count of followed users with status != 'PENDING'.
func (m *DBManager) GetProcessedFollowingCount(ctx context.Context) (int, error) {
	var count int
	err := m.sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM following_users WHERE status != 'PENDING'").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get processed following count: %w", err)
	}
	return count, nil
}

// ClearPendingFollowingUsers deletes all users with status 'PENDING'.
func (m *DBManager) ClearPendingFollowingUsers(ctx context.Context) error {
	_, err := m.sqlDB.ExecContext(ctx, "DELETE FROM following_users WHERE status = 'PENDING'")
	if err != nil {
		return fmt.Errorf("clear pending following users: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Followers Operations
// ----------------------------------------------------------------------------

// ReplaceFollowers atomically wipes the followers table and stores the new snapshot.
func (m *DBManager) ReplaceFollowers(ctx context.Context, userIDs []int64) error {
	return m.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM followers"); err != nil {
			return fmt.Errorf("delete old followers: %w", err)
		}

		if len(userIDs) == 0 {
			return nil
		}

		stmt, err := tx.PrepareContext(ctx, "INSERT INTO followers (user_id) VALUES (?)")
		if err != nil {
			return fmt.Errorf("prepare insert followers: %w", err)
		}
		defer stmt.Close()

		for _, id := range userIDs {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return fmt.Errorf("insert follower %d: %w", id, err)
			}
		}
		return nil
	})
}

// GetFollowerIDs retrieves all follower user IDs.
func (m *DBManager) GetFollowerIDs(ctx context.Context) ([]int64, error) {
	rows, err := m.sqlDB.QueryContext(ctx, "SELECT user_id FROM followers ORDER BY user_id ASC")
	if err != nil {
		return nil, fmt.Errorf("get follower ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan follower id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAllFollowerIDs is an alias for GetFollowerIDs.
func (m *DBManager) GetAllFollowerIDs(ctx context.Context) ([]int64, error) {
	return m.GetFollowerIDs(ctx)
}

// GetFollowerIDSet returns follower IDs as a map for O(1) set lookups.
func (m *DBManager) GetFollowerIDSet(ctx context.Context) (map[int64]bool, error) {
	ids, err := m.GetFollowerIDs(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// ----------------------------------------------------------------------------
// Unfollows Audit Operations
// ----------------------------------------------------------------------------

// LogUnfollows inserts multiple unfollow audit events in a transaction.
func (m *DBManager) LogUnfollows(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	return m.WithTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO unfollows (user_id) VALUES (?)")
		if err != nil {
			return fmt.Errorf("prepare log unfollows: %w", err)
		}
		defer stmt.Close()

		for _, id := range userIDs {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return fmt.Errorf("insert unfollow %d: %w", id, err)
			}
		}
		return nil
	})
}

// GetRecentUnfollows retrieves recently recorded unfollows.
func (m *DBManager) GetRecentUnfollows(ctx context.Context, limit int) ([]*Unfollow, error) {
	query := `SELECT id, user_id, timestamp FROM unfollows ORDER BY timestamp DESC, id DESC LIMIT ?`
	rows, err := m.sqlDB.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent unfollows: %w", err)
	}
	defer rows.Close()

	var unfollows []*Unfollow
	for rows.Next() {
		var u Unfollow
		var ts sql.NullString
		if err := rows.Scan(&u.ID, &u.UserID, &ts); err != nil {
			return nil, fmt.Errorf("scan unfollow row: %w", err)
		}
		if ts.Valid {
			u.Timestamp, _ = parseSQLiteTime(ts.String)
		}
		unfollows = append(unfollows, &u)
	}
	return unfollows, rows.Err()
}

// GetUnfollowsCount returns the total count of unfollow records.
func (m *DBManager) GetUnfollowsCount(ctx context.Context) (int, error) {
	var count int
	err := m.sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM unfollows").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get unfollows count: %w", err)
	}
	return count, nil
}

// ----------------------------------------------------------------------------
// Deleted Tweets Operations
// ----------------------------------------------------------------------------

// LogDeletedTweet inserts an audit record of a deleted tweet (ignoring duplicates).
func (m *DBManager) LogDeletedTweet(ctx context.Context, tweetID int64, text string, createdAt string, engagementScore int, isResponse bool) error {
	query := `
		INSERT OR IGNORE INTO deleted_tweets (tweet_id, text, created_at, engagement_score, is_response)
		VALUES (?, ?, ?, ?, ?)`
	_, err := m.sqlDB.ExecContext(ctx, query, tweetID, text, createdAt, engagementScore, isResponse)
	if err != nil {
		return fmt.Errorf("log deleted tweet %d: %w", tweetID, err)
	}
	return nil
}

// AddDeletedTweet is a convenience wrapper for DeletedTweet structs.
func (m *DBManager) AddDeletedTweet(ctx context.Context, tweet *DeletedTweet) error {
	return m.LogDeletedTweet(ctx, tweet.TweetID, tweet.Text, tweet.CreatedAt, tweet.EngagementScore, tweet.IsResponse)
}

// GetDeletedTweetIDs returns all logged deleted tweet IDs.
func (m *DBManager) GetDeletedTweetIDs(ctx context.Context) ([]int64, error) {
	rows, err := m.sqlDB.QueryContext(ctx, "SELECT tweet_id FROM deleted_tweets ORDER BY tweet_id ASC")
	if err != nil {
		return nil, fmt.Errorf("get deleted tweet ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deleted tweet id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAllDeletedTweetIDs is an alias for GetDeletedTweetIDs.
func (m *DBManager) GetAllDeletedTweetIDs(ctx context.Context) ([]int64, error) {
	return m.GetDeletedTweetIDs(ctx)
}

// GetDeletedTweetIDSet returns all deleted tweet IDs as a map for fast lookup.
func (m *DBManager) GetDeletedTweetIDSet(ctx context.Context) (map[int64]bool, error) {
	ids, err := m.GetDeletedTweetIDs(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// IsTweetDeleted checks if a specific tweet ID is recorded as deleted.
func (m *DBManager) IsTweetDeleted(ctx context.Context, tweetID int64) (bool, error) {
	var exists int
	err := m.sqlDB.QueryRowContext(ctx, "SELECT 1 FROM deleted_tweets WHERE tweet_id = ?", tweetID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check tweet deleted: %w", err)
	}
	return true, nil
}

// GetDeletedCount returns the total count of logged deleted tweets.
func (m *DBManager) GetDeletedCount(ctx context.Context) (int, error) {
	var count int
	err := m.sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM deleted_tweets").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get deleted count: %w", err)
	}
	return count, nil
}

// ----------------------------------------------------------------------------
// Schema Versions Operations
// ----------------------------------------------------------------------------

// GetAppliedVersions retrieves all recorded migration versions in ascending order.
func (m *DBManager) GetAppliedVersions(ctx context.Context) ([]*SchemaVersion, error) {
	query := `SELECT version, applied_at, description FROM schema_versions ORDER BY version ASC`
	rows, err := m.sqlDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get applied versions: %w", err)
	}
	defer rows.Close()

	var versions []*SchemaVersion
	for rows.Next() {
		var v SchemaVersion
		var ts sql.NullString
		if err := rows.Scan(&v.Version, &ts, &v.Description); err != nil {
			return nil, fmt.Errorf("scan schema version: %w", err)
		}
		if ts.Valid {
			v.AppliedAt, _ = parseSQLiteTime(ts.String)
		}
		versions = append(versions, &v)
	}
	return versions, rows.Err()
}
