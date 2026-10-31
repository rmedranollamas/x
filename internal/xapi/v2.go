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

// isForbiddenSuspended checks whether an HTTP 403 from v1.1 users/show.json represents
// a genuinely suspended account (code 63) rather than an API tier/access error (e.g. code 453).
func isForbiddenSuspended(body []byte, err error) bool {
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if strings.Contains(strings.ToLower(string(body)), "suspended") {
		return true
	}
	var fbErr *ForbiddenError
	if errors.As(err, &fbErr) && fbErr.ErrorCode == 63 {
		return true
	}
	var errPayload struct {
		Errors []struct {
			Code int `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &errPayload) == nil {
		for _, e := range errPayload.Errors {
			if e.Code == 63 {
				return true
			}
		}
	}
	return false
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
		if resp.StatusCode == http.StatusForbidden && isForbiddenSuspended(body, err) {
			return "(Suspended)"
		}
	}
	return "(Unknown)"
}

// extractMissingUserID extracts a user ID from a v2 partial batch error entry.
func extractMissingUserID(resourceID, parameter, value, detail string) int64 {
	if resourceID != "" {
		if id, err := strconv.ParseInt(strings.TrimSpace(resourceID), 10, 64); err == nil && id != 0 {
			return id
		}
	}
	if value != "" && (parameter == "" || parameter == "id" || parameter == "ids") {
		if id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && id != 0 {
			return id
		}
	}
	rem := detail
	for {
		start := strings.IndexByte(rem, '[')
		if start == -1 {
			break
		}
		end := strings.IndexByte(rem[start+1:], ']')
		if end == -1 {
			break
		}
		candidate := strings.TrimSpace(rem[start+1 : start+1+end])
		if candidate != "" {
			allDigits := true
			for i := 0; i < len(candidate); i++ {
				if candidate[i] < '0' || candidate[i] > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				if id, err := strconv.ParseInt(candidate, 10, 64); err == nil && id != 0 {
					return id
				}
			}
		}
		rem = rem[start+1:]
	}
	return 0
}

// GetUsersBatch fetches user details for a slice of user IDs using v2 /2/users, chunking in batches of up to 100.
func (c *Client) GetUsersBatch(ctx context.Context, userIDs []int64) (map[int64]*User, error) {
	if len(userIDs) == 0 {
		return make(map[int64]*User), nil
	}

	uniqueIDs := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, id := range userIDs {
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	result := make(map[int64]*User, len(uniqueIDs))

	const chunkSize = 100
	for i := 0; i < len(uniqueIDs); i += chunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		end := i + chunkSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		chunk := uniqueIDs[i:end]

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
				ResourceID string `json:"resource_id"`
				Parameter  string `json:"parameter"`
				Value      string `json:"value"`
				Detail     string `json:"detail"`
				Title      string `json:"title"`
				Type       string `json:"type"`
			} `json:"errors"`
		}

		if err := c.executeJSON(ctx, "GET /2/users", reqFactory, &resp); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			logging.Warnf("Error fetching users for chunk starting at %d: %v; falling back to v1.1 lookup", i, err)
			v1Users, reachable, v1Err := c.v1LookupUsersChunk(ctx, chunk)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if v1Err != nil {
				logging.Warnf("v1.1 batch user lookup failed for chunk starting at %d: %v", i, v1Err)
			}
			for id, u := range v1Users {
				result[id] = u
			}
			for idx, id := range chunk {
				if result[id] != nil {
					continue
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				fallbackStatus := c.ResolveUserFallback(ctx, id)
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				status := "ACTIVE"
				if strings.HasPrefix(fallbackStatus, "(") {
					status = fallbackStatus
				}
				result[id] = &User{
					ID:       id,
					IDStr:    strconv.FormatInt(id, 10),
					Username: fallbackStatus,
					Name:     fallbackStatus,
					Status:   status,
				}
				if !reachable && fallbackStatus == "(Unknown)" {
					for _, remID := range chunk[idx+1:] {
						if result[remID] == nil {
							result[remID] = &User{
								ID:       remID,
								IDStr:    strconv.FormatInt(remID, 10),
								Username: "(Unknown)",
								Name:     "(Unknown)",
								Status:   "(Unknown)",
							}
						}
					}
					break
				}
			}
			continue
		}

		for _, u := range resp.Data {
			user := u.ToDomainUser()
			result[user.ID] = user
		}

		errHints := make(map[int64]string, len(resp.Errors))
		for _, errItem := range resp.Errors {
			missingID := extractMissingUserID(errItem.ResourceID, errItem.Parameter, errItem.Value, errItem.Detail)
			if missingID == 0 {
				continue
			}
			combined := strings.ToLower(errItem.Detail + " " + errItem.Title)
			if strings.Contains(combined, "suspended") {
				errHints[missingID] = "(Suspended)"
			} else if strings.Contains(combined, "not find user") || strings.Contains(combined, "not found") {
				errHints[missingID] = "(Deactivated)"
			}
		}

		for _, id := range chunk {
			if result[id] != nil {
				continue
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			fallbackStatus := c.ResolveUserFallback(ctx, id)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if fallbackStatus == "(Unknown)" && errHints[id] != "" {
				fallbackStatus = errHints[id]
			}
			status := "ACTIVE"
			if strings.HasPrefix(fallbackStatus, "(") {
				status = fallbackStatus
			}
			result[id] = &User{
				ID:       id,
				IDStr:    strconv.FormatInt(id, 10),
				Username: fallbackStatus,
				Name:     fallbackStatus,
				Status:   status,
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
