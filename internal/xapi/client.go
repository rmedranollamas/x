package xapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dghubble/oauth1"
	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/logging"
)

// XClient defines the unified Twitter API interface contracts matching PROJECT.md.
type XClient interface {
	GetMe(ctx context.Context) (*User, error)
	GetBlockedUserIDs(ctx context.Context) ([]int64, error)
	UnblockUser(ctx context.Context, userID int64) (string, error)
	UnfollowUser(ctx context.Context, userID int64) (string, error)
	GetFollowerIDs(ctx context.Context) ([]int64, error)
	GetFriendIDs(ctx context.Context) ([]int64, error)
	GetUsersBatch(ctx context.Context, userIDs []int64) (map[int64]*User, error)
	GetUserTimeline(ctx context.Context, userID int64, count int, maxID int64) ([]*Tweet, error)
	DeleteTweet(ctx context.Context, tweetID int64) (bool, error)
}

// Compile-time interface compliance assertions.
var (
	_ XClient      = (*Client)(nil)
	_ RawAPIClient = (*Client)(nil)
)

// User represents a unified Twitter user account across v1.1 and v2 APIs.
type User struct {
	ID              int64     `json:"id"`
	IDStr           string    `json:"id_str"`
	Username        string    `json:"username"` // screen_name in v1.1, username in v2
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	FollowersCount  int       `json:"followers_count"`
	FollowingCount  int       `json:"following_count"`
	TweetCount      int       `json:"tweet_count"`
	ListedCount     int       `json:"listed_count"`
	ProfileImageURL string    `json:"profile_image_url"`
	CreatedAt       time.Time `json:"created_at"`
	PinnedTweetID   int64     `json:"pinned_tweet_id,omitempty"`
	Status          string    `json:"status,omitempty"` // ACTIVE, SUSPENDED, DEACTIVATED, NOT_FOUND
}

// Tweet represents a unified tweet status across v1.1 and v2 APIs.
type Tweet struct {
	ID                int64             `json:"id"`
	IDStr             string            `json:"id_str"`
	Text              string            `json:"text"`
	FullText          string            `json:"full_text"`
	CreatedAt         time.Time         `json:"created_at"`
	FavoriteCount     int               `json:"favorite_count"`
	RetweetCount      int               `json:"retweet_count"`
	InReplyToStatusID *int64            `json:"in_reply_to_status_id,omitempty"`
	InReplyToUserID   *int64            `json:"in_reply_to_user_id,omitempty"`
	PublicMetrics     PublicMetrics     `json:"public_metrics"`
	Entities          Entities          `json:"entities"`
	ExtendedEntities  ExtendedEntities  `json:"extended_entities"`
	ReferencedTweets  []ReferencedTweet `json:"referenced_tweets,omitempty"`
}

// PublicMetrics holds engagement metrics for a tweet.
type PublicMetrics struct {
	RetweetCount    int `json:"retweet_count"`
	ReplyCount      int `json:"reply_count"`
	LikeCount       int `json:"like_count"`
	QuoteCount      int `json:"quote_count"`
	ImpressionCount int `json:"impression_count"`
	BookmarkCount   int `json:"bookmark_count"`
}

// Entities holds rich metadata entities embedded in tweets.
type Entities struct {
	URLs     []EntityURL     `json:"urls,omitempty"`
	Media    []EntityMedia   `json:"media,omitempty"`
	Mentions []EntityMention `json:"user_mentions,omitempty"`
}

// ExtendedEntities holds extended media attachments.
type ExtendedEntities struct {
	Media []EntityMedia `json:"media,omitempty"`
}

// EntityURL represents an expanded URL inside a tweet.
type EntityURL struct {
	URL         string `json:"url"`
	ExpandedURL string `json:"expanded_url"`
	DisplayURL  string `json:"display_url"`
}

// EntityMedia represents a photo or video attached to a tweet.
type EntityMedia struct {
	ID       int64  `json:"id"`
	MediaURL string `json:"media_url_https"`
	Type     string `json:"type"`
	Expanded string `json:"expanded_url"`
}

// EntityMention represents a user mention inside a tweet.
type EntityMention struct {
	ID         int64  `json:"id"`
	ScreenName string `json:"screen_name"`
	Name       string `json:"name"`
}

// ReferencedTweet tracks reply, retweet, or quote parentage in API v2.
type ReferencedTweet struct {
	Type string `json:"type"` // replied_to, retweeted, quoted
	ID   string `json:"id"`
}

// APIError represents an error response from the Twitter API.
type APIError struct {
	StatusCode int         `json:"status_code"`
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	ErrorCode  int         `json:"error_code,omitempty"`
	Message    string      `json:"message"`
	Headers    http.Header `json:"headers"`
	RawBody    string      `json:"raw_body"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Twitter API error (%s %s): HTTP %d - %s", e.Method, e.URL, e.StatusCode, e.Message)
}

// RateLimitError represents an HTTP 429 Too Many Requests response.
type RateLimitError struct {
	APIError
	ResetTime      time.Time
	IsDailyLimit   bool
	DailyResetTime time.Time
	RetryAfter     time.Duration
}

func (e *RateLimitError) Error() string {
	if e.IsDailyLimit {
		return fmt.Sprintf("Twitter API daily rate limit reached. Resets at %s", e.DailyResetTime.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("Twitter API rate limit reached. Resets at %s", e.ResetTime.UTC().Format(time.RFC3339))
}

// NotFoundError represents an HTTP 404 Not Found response.
type NotFoundError struct {
	APIError
}

// ForbiddenError represents an HTTP 403 Forbidden response.
type ForbiddenError struct {
	APIError
}

// UnauthorizedError represents an HTTP 401 Unauthorized response.
type UnauthorizedError struct {
	APIError
}

// Error classification helpers
func IsRateLimit(err error) bool {
	var rl *RateLimitError
	return errors.As(err, &rl)
}

func IsNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}

func IsForbidden(err error) bool {
	var fb *ForbiddenError
	return errors.As(err, &fb)
}

func IsUnauthorized(err error) bool {
	var ua *UnauthorizedError
	return errors.As(err, &ua)
}

func IsTransient(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 && apiErr.StatusCode <= 599
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "broken pipe") ||
			strings.Contains(msg, "unexpected eof") ||
			strings.Contains(msg, "i/o timeout")
	}
	return false
}

// ParseAPIError parses an HTTP response and raw body into a structured error.
func ParseAPIError(resp *http.Response, body []byte, req *http.Request) error {
	base := APIError{
		StatusCode: resp.StatusCode,
		Method:     req.Method,
		URL:        req.URL.String(),
		Headers:    resp.Header,
		RawBody:    string(body),
		Message:    http.StatusText(resp.StatusCode),
	}

	var errResp struct {
		Errors []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
			Title   string `json:"title"`
		} `json:"errors"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		if len(errResp.Errors) > 0 {
			base.ErrorCode = errResp.Errors[0].Code
			if errResp.Errors[0].Detail != "" {
				base.Message = errResp.Errors[0].Detail
			} else if errResp.Errors[0].Message != "" {
				base.Message = errResp.Errors[0].Message
			} else if errResp.Errors[0].Title != "" {
				base.Message = errResp.Errors[0].Title
			}
		} else if errResp.Detail != "" {
			base.Message = errResp.Detail
		} else if errResp.Title != "" {
			base.Message = errResp.Title
		}
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		rlErr := &RateLimitError{APIError: base}
		appRem := resp.Header.Get("x-app-limit-24hour-remaining")
		userRem := resp.Header.Get("x-user-limit-24hour-remaining")
		appReset := resp.Header.Get("x-app-limit-24hour-reset")
		userReset := resp.Header.Get("x-user-limit-24hour-reset")

		if strings.TrimSpace(appRem) == "0" && appReset != "" {
			if ts, err := strconv.ParseInt(strings.TrimSpace(appReset), 10, 64); err == nil {
				rlErr.IsDailyLimit = true
				rlErr.DailyResetTime = time.Unix(ts, 0).UTC()
			}
		} else if strings.TrimSpace(userRem) == "0" && userReset != "" {
			if ts, err := strconv.ParseInt(strings.TrimSpace(userReset), 10, 64); err == nil {
				rlErr.IsDailyLimit = true
				rlErr.DailyResetTime = time.Unix(ts, 0).UTC()
			}
		}

		if resetStr := resp.Header.Get("x-rate-limit-reset"); resetStr != "" {
			if ts, err := strconv.ParseInt(strings.TrimSpace(resetStr), 10, 64); err == nil {
				rlErr.ResetTime = time.Unix(ts, 0).UTC()
			}
		}

		if ra := resp.Header.Get("retry-after"); ra != "" {
			if sec, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil {
				rlErr.RetryAfter = time.Duration(sec) * time.Second
			}
		}
		return rlErr

	case http.StatusNotFound:
		return &NotFoundError{APIError: base}
	case http.StatusForbidden:
		return &ForbiddenError{APIError: base}
	case http.StatusUnauthorized:
		return &UnauthorizedError{APIError: base}
	default:
		return &base
	}
}

// Client implements XClient and manages authenticated HTTP communication with Twitter APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
	config     *config.Config
	oauthCfg   *oauth1.Config
	token      *oauth1.Token

	resilience *ResilienceHandler
	zombie     *ZombieManager

	sleepFn SleepFn
	nowFn   NowFn

	v1Mu          sync.Mutex
	mu            sync.RWMutex
	authenticated bool
	meUser        *User
}

// Option configures a Client instance.
type Option func(*Client)

// WithBaseURL overrides the API base URL (useful for testing and mock servers).
func WithBaseURL(rawURL string) Option {
	return func(c *Client) {
		if rawURL != "" {
			c.baseURL = strings.TrimRight(rawURL, "/")
		}
	}
}

// WithHTTPClient replaces the internal standard HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithRoundTripper sets the underlying base RoundTripper for the OAuth1 transport.
func WithRoundTripper(rt http.RoundTripper) Option {
	return func(c *Client) {
		if rt != nil {
			if c.httpClient != nil {
				if t, ok := c.httpClient.Transport.(*oauth1.Transport); ok {
					t.Base = rt
					return
				}
			}
			// If transport is not yet oauth1 or httpClient was replaced, wrap with transport
			if c.oauthCfg != nil && c.token != nil {
				ctx := context.WithValue(context.Background(), oauth1.HTTPClient, &http.Client{Transport: rt})
				c.httpClient = c.oauthCfg.Client(ctx, c.token)
				c.httpClient.Timeout = 60 * time.Second
			} else {
				c.httpClient = &http.Client{
					Transport: rt,
					Timeout:   60 * time.Second,
				}
			}
		}
	}
}

// WithTimeout configures the request timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if c.httpClient != nil {
			c.httpClient.Timeout = timeout
		}
	}
}

// WithSleep configures a custom sleep function (e.g. for sub-millisecond tests).
func WithSleep(fn SleepFn) Option {
	return func(c *Client) {
		c.sleepFn = fn
	}
}

// WithNow configures a custom clock function for unit testing.
func WithNow(fn NowFn) Option {
	return func(c *Client) {
		c.nowFn = fn
	}
}

// WithResilienceConfig configures custom resilience parameters.
func WithResilienceConfig(rCfg ResilienceConfig) Option {
	return func(c *Client) {
		if c.resilience != nil {
			c.resilience.config = rCfg
		}
	}
}

// NewClient initializes an authenticated Client using OAuth 1.0a credentials.
func NewClient(cfg *config.Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		cfg = &config.Config{
			XAPIKey:            "test_api_key",
			XAPIKeySecret:      "test_api_secret",
			XAccessToken:       "test_token",
			XAccessTokenSecret: "test_token_secret",
		}
	} else {
		// Only check config if not explicitly overridden by test options
		// We validate credentials
		if err := cfg.CheckConfig(); err != nil {
			// Check if any option is providing a mock transport
			hasMock := false
			testClient := &Client{}
			for _, opt := range opts {
				opt(testClient)
			}
			if testClient.httpClient != nil {
				hasMock = true
			}
			if !hasMock {
				return nil, err
			}
		}
	}

	oauthCfg := oauth1.NewConfig(cfg.XAPIKey, cfg.XAPIKeySecret)
	token := oauth1.NewToken(cfg.XAccessToken, cfg.XAccessTokenSecret)

	httpClient := oauthCfg.Client(context.Background(), token)
	if t, ok := httpClient.Transport.(*oauth1.Transport); ok {
		t.Base = http.DefaultTransport
	}
	httpClient.Timeout = 60 * time.Second

	baseURL := "https://api.twitter.com"
	if envURL := os.Getenv("TWITTER_API_BASE_URL"); envURL != "" {
		baseURL = strings.TrimRight(envURL, "/")
	}

	resCfg := DefaultResilienceConfig()

	c := &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		config:     cfg,
		oauthCfg:   oauthCfg,
		token:      token,
		sleepFn:    DefaultSleep,
		nowFn:      func() time.Time { return time.Now().UTC() },
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	// Update resilience handler with configured sleep/now functions
	resCfg.NowFunc = c.nowFn
	c.resilience = NewResilienceHandler(resCfg, c.httpClient, c.sleepFn)
	c.zombie = NewZombieManager(c)

	return c, nil
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// HTTPClient returns the underlying http.Client.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// GetAuthenticatedUserID returns the authenticated user's ID, caching it if already fetched.
func (c *Client) GetAuthenticatedUserID(ctx context.Context) (int64, error) {
	c.mu.RLock()
	if c.authenticated && c.meUser != nil && c.meUser.ID != 0 {
		id := c.meUser.ID
		c.mu.RUnlock()
		return id, nil
	}
	c.mu.RUnlock()

	me, err := c.GetMe(ctx)
	if err != nil {
		return 0, err
	}
	return me.ID, nil
}

// executeRequest performs an HTTP request with full resilience (rate limit pause + transient retry).
func (c *Client) executeRequest(
	ctx context.Context,
	operationName string,
	reqFactory func() (*http.Request, error),
) (*http.Response, []byte, error) {
	resp, err := c.resilience.Execute(ctx, operationName, reqFactory)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		sampleReq, _ := reqFactory()
		if sampleReq == nil {
			sampleReq, _ = http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
		}
		return resp, body, ParseAPIError(resp, body, sampleReq)
	}

	return resp, body, nil
}

// executeJSON wraps executeRequest and deserializes response JSON into target.
func (c *Client) executeJSON(
	ctx context.Context,
	operationName string,
	reqFactory func() (*http.Request, error),
	target interface{},
) error {
	_, body, err := c.executeRequest(ctx, operationName, reqFactory)
	if err != nil {
		return err
	}
	if target != nil && len(body) > 0 {
		if err := json.Unmarshal(body, target); err != nil {
			logging.Debug(fmt.Sprintf("JSON unmarshal error on body: %s", string(body)))
			return fmt.Errorf("failed to decode JSON response: %w (body: %s)", err, string(body))
		}
	}
	return nil
}
