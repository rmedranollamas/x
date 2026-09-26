package xapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rmedranollamas/x-agent/internal/logging"
)

// flexibleIDSlice handles unmarshaling JSON arrays of either numbers or strings.
type flexibleIDSlice []int64

func (f *flexibleIDSlice) UnmarshalJSON(data []byte) error {
	var ints []int64
	if err := json.Unmarshal(data, &ints); err == nil {
		*f = ints
		return nil
	}

	var strs []string
	if err := json.Unmarshal(data, &strs); err == nil {
		res := make([]int64, 0, len(strs))
		for _, s := range strs {
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				res = append(res, id)
			}
		}
		*f = res
		return nil
	}

	var mixed []interface{}
	if err := json.Unmarshal(data, &mixed); err == nil {
		res := make([]int64, 0, len(mixed))
		for _, item := range mixed {
			switch v := item.(type) {
			case float64:
				res = append(res, int64(v))
			case string:
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					res = append(res, id)
				}
			}
		}
		*f = res
		return nil
	}

	return fmt.Errorf("cannot unmarshal IDs into []int64: %s", string(data))
}

type v1CursorIDsResponse struct {
	IDs            flexibleIDSlice `json:"ids"`
	NextCursor     int64           `json:"next_cursor"`
	NextCursorStr  string          `json:"next_cursor_str"`
	PreviousCursor int64           `json:"previous_cursor"`
}

// fetchAllIDsWithCursor retrieves all IDs across paginated v1.1 cursor responses.
func (c *Client) fetchAllIDsWithCursor(ctx context.Context, endpointPath string, label string) ([]int64, error) {
	var allIDs []int64
	seen := make(map[int64]struct{})
	cursor := int64(-1)

	logging.Infof("Fetching %s via v1.1 API...", label)

	for cursor != 0 {
		currentCursor := cursor
		reqFactory := func() (*http.Request, error) {
			query := url.Values{}
			query.Set("cursor", strconv.FormatInt(currentCursor, 10))
			query.Set("stringify_ids", "true")
			query.Set("count", "5000")

			reqURL := fmt.Sprintf("%s%s?%s", c.baseURL, endpointPath, query.Encode())
			return http.NewRequest(http.MethodGet, reqURL, nil)
		}

		var resp v1CursorIDsResponse
		if err := c.executeJSON(ctx, fmt.Sprintf("GET %s", endpointPath), reqFactory, &resp); err != nil {
			return nil, err
		}

		for _, id := range resp.IDs {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				allIDs = append(allIDs, id)
			}
		}

		if resp.NextCursorStr != "" && resp.NextCursorStr != "0" {
			cursor, _ = strconv.ParseInt(resp.NextCursorStr, 10, 64)
		} else {
			cursor = resp.NextCursor
		}

		if len(allIDs)%1000 == 0 || cursor == 0 {
			logging.SingleLinef("Fetched %d IDs...", len(allIDs))
		}
	}

	logging.Infof("Finished fetching %s. Found a total of %d account IDs.", label, len(allIDs))
	return allIDs, nil
}

// GetBlockedUserIDs fetches all blocked user IDs using Twitter API v1.1.
func (c *Client) GetBlockedUserIDs(ctx context.Context) ([]int64, error) {
	c.v1Mu.Lock()
	defer c.v1Mu.Unlock()
	return c.fetchAllIDsWithCursor(ctx, "/1.1/blocks/ids.json", "blocked account IDs")
}

// GetFollowerIDs fetches all follower user IDs using Twitter API v1.1.
func (c *Client) GetFollowerIDs(ctx context.Context) ([]int64, error) {
	c.v1Mu.Lock()
	defer c.v1Mu.Unlock()
	return c.fetchAllIDsWithCursor(ctx, "/1.1/followers/ids.json", "follower account IDs")
}

// GetFriendIDs fetches all following user IDs using Twitter API v1.1.
func (c *Client) GetFriendIDs(ctx context.Context) ([]int64, error) {
	c.v1Mu.Lock()
	defer c.v1Mu.Unlock()
	return c.fetchAllIDsWithCursor(ctx, "/1.1/friends/ids.json", "following account IDs")
}

// UnfollowUser unfollows a target user ID using Twitter API v1.1 friendships/destroy.
func (c *Client) UnfollowUser(ctx context.Context, userID int64) (string, error) {
	c.v1Mu.Lock()
	defer c.v1Mu.Unlock()

	reqFactory := func() (*http.Request, error) {
		form := url.Values{}
		form.Set("user_id", strconv.FormatInt(userID, 10))
		reqURL := fmt.Sprintf("%s/1.1/friendships/destroy.json", c.baseURL)
		req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}

	var dummy map[string]interface{}
	if err := c.executeJSON(ctx, "POST /1.1/friendships/destroy.json", reqFactory, &dummy); err != nil {
		logging.Warnf("Failed to unfollow %d: %v", userID, err)
		return "FAILED", err
	}

	return "SUCCESS", nil
}

// v1Status defines the JSON schema returned by statuses/user_timeline.json.
type v1Status struct {
	ID                int64            `json:"id"`
	IDStr             string           `json:"id_str"`
	Text              string           `json:"text"`
	FullText          string           `json:"full_text"`
	CreatedAt         string           `json:"created_at"`
	FavoriteCount     int              `json:"favorite_count"`
	RetweetCount      int              `json:"retweet_count"`
	InReplyToStatusID *int64           `json:"in_reply_to_status_id"`
	InReplyToUserID   *int64           `json:"in_reply_to_user_id"`
	Entities          Entities         `json:"entities"`
	ExtendedEntities  ExtendedEntities `json:"extended_entities"`
}

// parseV1Time parses Twitter v1.1 RubyDate timestamps into time.Time.
func parseV1Time(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RubyDate, // "Mon Jan 02 15:04:05 -0700 2006"
		time.RFC1123Z,
		time.RFC3339,
		"Mon Jan 02 15:04:05 +0000 2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// GetUserTimeline fetches a page of tweets using Twitter API v1.1 statuses/user_timeline.
func (c *Client) GetUserTimeline(ctx context.Context, userID int64, count int, maxID int64) ([]*Tweet, error) {
	c.v1Mu.Lock()
	defer c.v1Mu.Unlock()

	if count <= 0 {
		count = 200
	}

	reqFactory := func() (*http.Request, error) {
		query := url.Values{}
		query.Set("user_id", strconv.FormatInt(userID, 10))
		query.Set("count", strconv.Itoa(count))
		query.Set("include_rts", "true")
		query.Set("tweet_mode", "extended")
		if maxID > 0 {
			query.Set("max_id", strconv.FormatInt(maxID, 10))
		}

		reqURL := fmt.Sprintf("%s/1.1/statuses/user_timeline.json?%s", c.baseURL, query.Encode())
		return http.NewRequest(http.MethodGet, reqURL, nil)
	}

	var rawStatuses []v1Status
	if err := c.executeJSON(ctx, "GET /1.1/statuses/user_timeline.json", reqFactory, &rawStatuses); err != nil {
		return nil, err
	}

	tweets := make([]*Tweet, len(rawStatuses))
	for i, s := range rawStatuses {
		txt := s.FullText
		if txt == "" {
			txt = s.Text
		}
		t := parseV1Time(s.CreatedAt)
		tweets[i] = &Tweet{
			ID:                s.ID,
			IDStr:             s.IDStr,
			Text:              txt,
			FullText:          txt,
			CreatedAt:         t,
			FavoriteCount:     s.FavoriteCount,
			RetweetCount:      s.RetweetCount,
			InReplyToStatusID: s.InReplyToStatusID,
			InReplyToUserID:   s.InReplyToUserID,
			Entities:          s.Entities,
			ExtendedEntities:  s.ExtendedEntities,
			PublicMetrics: PublicMetrics{
				LikeCount:    s.FavoriteCount,
				RetweetCount: s.RetweetCount,
			},
		}
	}
	return tweets, nil
}

// V1DestroyBlock sends POST /1.1/blocks/destroy.json to unblock a user.
func (c *Client) V1DestroyBlock(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	reqFactory := func() (*http.Request, error) {
		form := url.Values{}
		form.Set("user_id", strconv.FormatInt(targetUserID, 10))
		reqURL := fmt.Sprintf("%s/1.1/blocks/destroy.json", c.baseURL)
		req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}
	return c.executeRequest(ctx, "POST /1.1/blocks/destroy.json", reqFactory)
}

// V1CreateBlock sends POST /1.1/blocks/create.json to block a user.
func (c *Client) V1CreateBlock(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	reqFactory := func() (*http.Request, error) {
		form := url.Values{}
		form.Set("user_id", strconv.FormatInt(targetUserID, 10))
		reqURL := fmt.Sprintf("%s/1.1/blocks/create.json", c.baseURL)
		req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}
	return c.executeRequest(ctx, "POST /1.1/blocks/create.json", reqFactory)
}

// V1GetUser sends GET /1.1/users/show.json?user_id=... to verify user existence.
func (c *Client) V1GetUser(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	reqFactory := func() (*http.Request, error) {
		reqURL := fmt.Sprintf("%s/1.1/users/show.json?user_id=%d", c.baseURL, targetUserID)
		return http.NewRequest(http.MethodGet, reqURL, nil)
	}
	return c.executeRequest(ctx, "GET /1.1/users/show.json", reqFactory)
}
