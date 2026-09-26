package xapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rmedranollamas/x-agent/internal/logging"
)

// flexibleString allows JSON values that may be encoded as strings or numbers.
type flexibleString string

func (f *flexibleString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = flexibleString(s)
		return nil
	}
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*f = flexibleString(strconv.FormatInt(i, 10))
		return nil
	}
	var fl float64
	if err := json.Unmarshal(data, &fl); err == nil {
		*f = flexibleString(strconv.FormatInt(int64(fl), 10))
		return nil
	}
	return fmt.Errorf("cannot unmarshal into string: %s", string(data))
}

type v2UserData struct {
	ID            flexibleString `json:"id"`
	Name          string         `json:"name"`
	Username      string         `json:"username"`
	CreatedAt     time.Time      `json:"created_at"`
	PinnedTweetID flexibleString `json:"pinned_tweet_id"`
	Description   string         `json:"description"`
	PublicMetrics struct {
		FollowersCount int `json:"followers_count"`
		FollowingCount int `json:"following_count"`
		TweetCount     int `json:"tweet_count"`
		ListedCount    int `json:"listed_count"`
	} `json:"public_metrics"`
}

func (u *v2UserData) ToDomainUser() *User {
	idStr := string(u.ID)
	id, _ := strconv.ParseInt(idStr, 10, 64)
	pinnedID, _ := strconv.ParseInt(string(u.PinnedTweetID), 10, 64)
	return &User{
		ID:             id,
		IDStr:          idStr,
		Username:       u.Username,
		Name:           u.Name,
		Description:    u.Description,
		FollowersCount: u.PublicMetrics.FollowersCount,
		FollowingCount: u.PublicMetrics.FollowingCount,
		TweetCount:     u.PublicMetrics.TweetCount,
		ListedCount:    u.PublicMetrics.ListedCount,
		CreatedAt:      u.CreatedAt.UTC(),
		PinnedTweetID:  pinnedID,
		Status:         "ACTIVE",
	}
}

// GetMe retrieves the authenticated user's profile using Twitter API v2 /2/users/me.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	reqFactory := func() (*http.Request, error) {
		query := url.Values{}
		query.Set("user.fields", "public_metrics,created_at,description,pinned_tweet_id")
		reqURL := fmt.Sprintf("%s/2/users/me?%s", c.baseURL, query.Encode())
		return http.NewRequest(http.MethodGet, reqURL, nil)
	}

	var resp struct {
		Data *v2UserData `json:"data"`
	}
	if err := c.executeJSON(ctx, "GET /2/users/me", reqFactory, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, errors.New("no user data returned from /2/users/me")
	}

	user := resp.Data.ToDomainUser()

	c.mu.Lock()
	c.meUser = user
	c.authenticated = true
	c.mu.Unlock()

	logging.Infof("Authenticated as %s (ID: %d)", user.Username, user.ID)
	return user, nil
}

// ResolveUserFallback attempts to resolve a user handle via v1.1 or returns their status.
func (c *Client) ResolveUserFallback(ctx context.Context, userID int64) string {
	resp, body, err := c.V1GetUser(ctx, userID)
	if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
		var v1User struct {
			ScreenName string `json:"screen_name"`
		}
		if json.Unmarshal(body, &v1User) == nil && v1User.ScreenName != "" {
			return v1User.ScreenName
		}
		return fmt.Sprintf("user_%d", userID)
	}

	if resp != nil {
		if resp.StatusCode == http.StatusNotFound {
			return "(Deactivated)"
		}
		if resp.StatusCode == http.StatusForbidden {
			return "(Suspended)"
		}
	}
	return "(Unknown)"
}

// GetUsersBatch fetches user details for a slice of user IDs using v2 /2/users, chunking in batches of up to 100.
func (c *Client) GetUsersBatch(ctx context.Context, userIDs []int64) (map[int64]*User, error) {
	result := make(map[int64]*User, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	const chunkSize = 100
	for i := 0; i < len(userIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		chunk := userIDs[i:end]

		idStrs := make([]string, len(chunk))
		for idx, id := range chunk {
			idStrs[idx] = strconv.FormatInt(id, 10)
		}

		currentIDsStr := strings.Join(idStrs, ",")
		reqFactory := func() (*http.Request, error) {
			query := url.Values{}
			query.Set("ids", currentIDsStr)
			query.Set("user.fields", "public_metrics,created_at,description")
			reqURL := fmt.Sprintf("%s/2/users?%s", c.baseURL, query.Encode())
			return http.NewRequest(http.MethodGet, reqURL, nil)
		}

		var resp struct {
			Data   []v2UserData `json:"data"`
			Errors []struct {
				Parameter string `json:"parameter"`
				Value     string `json:"value"`
				Detail    string `json:"detail"`
				Title     string `json:"title"`
				Type      string `json:"type"`
			} `json:"errors"`
		}

		if err := c.executeJSON(ctx, "GET /2/users", reqFactory, &resp); err != nil {
			logging.Warnf("Error fetching users for chunk starting at %d: %v", i, err)
			continue
		}

		for _, u := range resp.Data {
			user := u.ToDomainUser()
			result[user.ID] = user
		}

		for _, errItem := range resp.Errors {
			var missingID int64
			if errItem.Parameter == "id" && errItem.Value != "" {
				missingID, _ = strconv.ParseInt(errItem.Value, 10, 64)
			} else if strings.Contains(errItem.Detail, "Could not find user with id [") {
				start := strings.Index(errItem.Detail, "[")
				end := strings.Index(errItem.Detail, "]")
				if start != -1 && end != -1 && end > start {
					missingID, _ = strconv.ParseInt(errItem.Detail[start+1:end], 10, 64)
				}
			}

			if missingID != 0 {
				fallbackStatus := c.ResolveUserFallback(ctx, missingID)
				result[missingID] = &User{
					ID:       missingID,
					IDStr:    strconv.FormatInt(missingID, 10),
					Username: fallbackStatus,
					Name:     fallbackStatus,
					Status:   fallbackStatus,
				}
			}
		}
	}

	return result, nil
}

// DeleteTweet deletes a tweet by ID using Twitter API v2 DELETE /2/tweets/:id.
func (c *Client) DeleteTweet(ctx context.Context, tweetID int64) (bool, error) {
	reqFactory := func() (*http.Request, error) {
		reqURL := fmt.Sprintf("%s/2/tweets/%d", c.baseURL, tweetID)
		return http.NewRequest(http.MethodDelete, reqURL, nil)
	}

	var resp struct {
		Data struct {
			Deleted bool `json:"deleted"`
		} `json:"data"`
	}

	err := c.executeJSON(ctx, fmt.Sprintf("DELETE /2/tweets/%d", tweetID), reqFactory, &resp)
	if err != nil {
		if IsNotFound(err) {
			return false, nil // Idempotent deletion
		}
		logging.Warnf("Failed to delete tweet %d: %v", tweetID, err)
		return false, err
	}

	return resp.Data.Deleted, nil
}

// V2Unblock deletes a block relationship using DELETE /2/users/:source/blocking/:target.
func (c *Client) V2Unblock(ctx context.Context, sourceUserID, targetUserID int64) (*http.Response, []byte, error) {
	reqFactory := func() (*http.Request, error) {
		reqURL := fmt.Sprintf("%s/2/users/%d/blocking/%d", c.baseURL, sourceUserID, targetUserID)
		return http.NewRequest(http.MethodDelete, reqURL, nil)
	}
	return c.executeRequest(ctx, "DELETE /2/users/:src/blocking/:tgt", reqFactory)
}

// V2Block creates a block relationship using POST /2/users/:source/blocking.
func (c *Client) V2Block(ctx context.Context, sourceUserID, targetUserID int64) (*http.Response, []byte, error) {
	reqFactory := func() (*http.Request, error) {
		payload := map[string]string{
			"target_user_id": strconv.FormatInt(targetUserID, 10),
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reqURL := fmt.Sprintf("%s/2/users/%d/blocking", c.baseURL, sourceUserID)
		req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}
	return c.executeRequest(ctx, "POST /2/users/:src/blocking", reqFactory)
}

// V2GetUser fetches user details using GET /2/users/:id to verify account existence.
func (c *Client) V2GetUser(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	reqFactory := func() (*http.Request, error) {
		query := url.Values{}
		query.Set("user.fields", "public_metrics,created_at,description")
		reqURL := fmt.Sprintf("%s/2/users/%d?%s", c.baseURL, targetUserID, query.Encode())
		return http.NewRequest(http.MethodGet, reqURL, nil)
	}
	return c.executeRequest(ctx, "GET /2/users/:id", reqFactory)
}
