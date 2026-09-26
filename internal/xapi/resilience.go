package xapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/dghubble/oauth1"
	"github.com/rmedranollamas/x-agent/internal/logging"
)

// SleepFn defines the function signature for sleeping during retries or rate limits.
type SleepFn func(ctx context.Context, d time.Duration) error

// NowFn defines the function signature for querying the current UTC time.
type NowFn func() time.Time

// DefaultSleep pauses execution until duration expires or context cancels.
func DefaultSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ResilienceConfig holds tuning parameters for rate limiting and retry backoff.
type ResilienceConfig struct {
	MaxTransientRetries uint64
	InitialBackoff      time.Duration
	MaxBackoff          time.Duration
	BackoffMultiplier   float64
	RateLimitBuffer15m  time.Duration
	DailyLimitBuffer    time.Duration
	PastTimestampSleep  time.Duration
	NoHeaderSleep       time.Duration
	NowFunc             NowFn
}

// DefaultResilienceConfig returns production defaults matching Python x_service parity.
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		MaxTransientRetries: 3,
		InitialBackoff:      1 * time.Second,
		MaxBackoff:          10 * time.Second,
		BackoffMultiplier:   2.0,
		RateLimitBuffer15m:  5 * time.Second,
		DailyLimitBuffer:    60 * time.Second,
		PastTimestampSleep:  1801 * time.Second, // 30m + 1s
		NoHeaderSleep:       901 * time.Second,  // 15m + 1s
		NowFunc:             func() time.Time { return time.Now().UTC() },
	}
}

// RateLimitDecision represents the calculated pause from rate limit headers.
type RateLimitDecision struct {
	IsRateLimited bool
	IsDailyLimit  bool
	ResetTime     time.Time
	WaitDuration  time.Duration
	Reason        string
}

// ResilienceHandler executes HTTP operations with rate limit pauses and backoff retries.
type ResilienceHandler struct {
	config     ResilienceConfig
	httpClient *http.Client
	sleep      SleepFn
}

// NewResilienceHandler constructs a configured ResilienceHandler.
func NewResilienceHandler(cfg ResilienceConfig, client *http.Client, sleep SleepFn) *ResilienceHandler {
	if client == nil {
		client = http.DefaultClient
	}
	if sleep == nil {
		sleep = DefaultSleep
	}
	if cfg.NowFunc == nil {
		cfg.NowFunc = func() time.Time { return time.Now().UTC() }
	}
	return &ResilienceHandler{
		config:     cfg,
		httpClient: client,
		sleep:      sleep,
	}
}

// IsTransientError checks whether an HTTP status or network error is retryable.
func IsTransientError(err error, statusCode int) bool {
	// 429 is NOT transient: rate limit handled separately
	if statusCode >= 500 && statusCode <= 599 {
		return true
	}
	if err == nil {
		return false
	}
	// Context cancellations are NEVER transient
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "i/o timeout")
}

// ParseRateLimitHeaders inspects response headers and computes required sleep duration.
func (h *ResilienceHandler) ParseRateLimitHeaders(header http.Header) RateLimitDecision {
	now := h.config.NowFunc()

	// 1. Clock skew calculation via Date header
	var serverSkew time.Duration
	if dateHeader := header.Get("Date"); dateHeader != "" {
		if serverTime, err := http.ParseTime(dateHeader); err == nil {
			serverSkew = now.Sub(serverTime.UTC())
		}
	}

	// 2. Check 24-hour daily limits
	appRemaining := header.Get("x-app-limit-24hour-remaining")
	appReset := header.Get("x-app-limit-24hour-reset")
	userRemaining := header.Get("x-user-limit-24hour-remaining")
	userReset := header.Get("x-user-limit-24hour-reset")

	var dailyResetEpoch int64
	if strings.TrimSpace(appRemaining) == "0" && appReset != "" {
		dailyResetEpoch, _ = strconv.ParseInt(strings.TrimSpace(appReset), 10, 64)
	} else if strings.TrimSpace(userRemaining) == "0" && userReset != "" {
		dailyResetEpoch, _ = strconv.ParseInt(strings.TrimSpace(userReset), 10, 64)
	}

	if dailyResetEpoch > 0 {
		resetTime := time.Unix(dailyResetEpoch, 0).UTC().Add(serverSkew)
		waitDuration := resetTime.Sub(now) + h.config.DailyLimitBuffer
		if waitDuration < h.config.DailyLimitBuffer {
			waitDuration = h.config.DailyLimitBuffer
		}
		return RateLimitDecision{
			IsRateLimited: true,
			IsDailyLimit:  true,
			ResetTime:     resetTime,
			WaitDuration:  waitDuration,
			Reason:        fmt.Sprintf("Daily quota reached. Resets at %s", resetTime.Format("2006-01-02 15:04:05 UTC")),
		}
	}

	// 3. Check 15-minute rolling window limits
	resetHeader := header.Get("x-rate-limit-reset")
	retryAfter := header.Get("retry-after")

	if resetHeader != "" {
		if epoch, err := strconv.ParseInt(strings.TrimSpace(resetHeader), 10, 64); err == nil {
			resetTime := time.Unix(epoch, 0).UTC().Add(serverSkew)
			diff := resetTime.Sub(now)
			if diff > 0 {
				return RateLimitDecision{
					IsRateLimited: true,
					IsDailyLimit:  false,
					ResetTime:     resetTime,
					WaitDuration:  diff + h.config.RateLimitBuffer15m,
					Reason:        fmt.Sprintf("15-minute window limit hit. Resets at %s", resetTime.Format("2006-01-02 15:04:05 UTC")),
				}
			}
			// Reset timestamp is in the past! Fallback to 30 minutes
			return RateLimitDecision{
				IsRateLimited: true,
				IsDailyLimit:  false,
				ResetTime:     now.Add(h.config.PastTimestampSleep),
				WaitDuration:  h.config.PastTimestampSleep,
				Reason:        "Reset time is in the past (potential daily cap). Falling back to 30-minute backoff",
			}
		}
	}

	// 4. Check retry-after header
	if retryAfter != "" {
		if seconds, err := strconv.ParseInt(strings.TrimSpace(retryAfter), 10, 64); err == nil {
			dur := time.Duration(seconds)*time.Second + h.config.RateLimitBuffer15m
			return RateLimitDecision{
				IsRateLimited: true,
				IsDailyLimit:  false,
				ResetTime:     now.Add(dur),
				WaitDuration:  dur,
				Reason:        fmt.Sprintf("Retry-After header specified %ds wait", seconds),
			}
		}
		if t, err := http.ParseTime(retryAfter); err == nil {
			dur := t.UTC().Sub(now) + h.config.RateLimitBuffer15m
			if dur > 0 {
				return RateLimitDecision{
					IsRateLimited: true,
					IsDailyLimit:  false,
					ResetTime:     t.UTC(),
					WaitDuration:  dur,
					Reason:        fmt.Sprintf("Retry-After date specified wait until %s", t.Format(time.RFC3339)),
				}
			}
		}
	}

	// 5. Fallback if no rate limit header was present
	return RateLimitDecision{
		IsRateLimited: true,
		IsDailyLimit:  false,
		ResetTime:     now.Add(h.config.NoHeaderSleep),
		WaitDuration:  h.config.NoHeaderSleep,
		Reason:        "No rate limit headers found. Falling back to default 15-minute sleep",
	}
}

// closeIdleConnections flushes idle connections in the transport.
func (h *ResilienceHandler) closeIdleConnections() {
	if tr, ok := h.httpClient.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	} else if oauthTr, ok := h.httpClient.Transport.(*oauth1.Transport); ok {
		if baseTr, ok := oauthTr.Base.(*http.Transport); ok {
			baseTr.CloseIdleConnections()
		}
	}
}

// Execute performs an HTTP operation with rate limit pauses and exponential backoff retries.
func (h *ResilienceHandler) Execute(
	ctx context.Context,
	operationName string,
	reqFactory func() (*http.Request, error),
) (*http.Response, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		b := backoff.NewExponentialBackOff()
		b.InitialInterval = h.config.InitialBackoff
		b.MaxInterval = h.config.MaxBackoff
		b.Multiplier = h.config.BackoffMultiplier
		b.RandomizationFactor = 0.5
		b.Reset()

		var lastResp *http.Response
		var lastErr error
		rateLimited := false

		for attempt := uint64(0); attempt <= h.config.MaxTransientRetries; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			req, err := reqFactory()
			if err != nil {
				return nil, fmt.Errorf("request factory failed: %w", err)
			}
			req = req.WithContext(ctx)

			resp, doErr := h.httpClient.Do(req)
			if doErr != nil {
				lastErr = doErr
				if IsTransientError(doErr, 0) {
					logging.Warnf("Transient network error for %s (attempt %d/%d): %v", operationName, attempt+1, h.config.MaxTransientRetries+1, doErr)
					if attempt < h.config.MaxTransientRetries {
						waitDur := b.NextBackOff()
						if waitDur != backoff.Stop {
							if sleepErr := h.sleep(ctx, waitDur); sleepErr != nil {
								return nil, sleepErr
							}
						}
						continue
					}
				}
				// Non-transient or retries exhausted
				return nil, lastErr
			}

			// Check HTTP 429 Too Many Requests
			if resp.StatusCode == http.StatusTooManyRequests {
				lastResp = resp
				rateLimited = true
				break
			}

			// Check HTTP 5xx Server Error
			if IsTransientError(nil, resp.StatusCode) {
				lastErr = fmt.Errorf("transient HTTP error: status %d", resp.StatusCode)
				_ = resp.Body.Close()
				logging.Warnf("Transient HTTP %d for %s (attempt %d/%d)", resp.StatusCode, operationName, attempt+1, h.config.MaxTransientRetries+1)
				if attempt < h.config.MaxTransientRetries {
					waitDur := b.NextBackOff()
					if waitDur != backoff.Stop {
						if sleepErr := h.sleep(ctx, waitDur); sleepErr != nil {
							return nil, sleepErr
						}
					}
					continue
				}
				return nil, fmt.Errorf("operation %s failed after transient retries: %w", operationName, lastErr)
			}

			// 2xx or non-transient 4xx
			return resp, nil
		}

		if rateLimited && lastResp != nil {
			decision := h.ParseRateLimitHeaders(lastResp.Header)
			_ = lastResp.Body.Close()

			if decision.IsDailyLimit {
				logging.Errorf("🛑 DAILY RATE LIMIT REACHED (v2). Quota will reset at %s. Sleeping for %.1f hours...",
					decision.ResetTime.Format("2006-01-02 15:04:05 UTC"), decision.WaitDuration.Hours())
			} else {
				if strings.Contains(decision.Reason, "past") {
					logging.Warn("Reset time is in the past. This often indicates a Daily Limit.")
					logging.Warn("Sleeping for 30 minutes as backoff...")
				} else {
					logging.Warnf("Rate limit hit. Resets at %s. Sleeping for %.0f seconds...",
						decision.ResetTime.Format("2006-01-02 15:04:05 UTC"), decision.WaitDuration.Seconds())
				}
			}

			if err := h.sleep(ctx, decision.WaitDuration); err != nil {
				return nil, fmt.Errorf("rate limit sleep interrupted by context: %w", err)
			}

			h.closeIdleConnections()
			// Retry outer loop after sleep
			continue
		}

		if lastErr != nil {
			return nil, fmt.Errorf("operation %s failed after transient retries: %w", operationName, lastErr)
		}
		if lastResp != nil {
			return lastResp, nil
		}
	}
}
