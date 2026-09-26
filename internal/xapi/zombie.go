package xapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/rmedranollamas/x-agent/internal/logging"
)

// UnblockStatus defines the possible outcomes of an unblock operation.
type UnblockStatus string

const (
	StatusUnblocked UnblockStatus = "UNBLOCKED"
	StatusNotFound  UnblockStatus = "NOT_FOUND"
	StatusFailed    UnblockStatus = "FAILED"
	StatusSuccess   UnblockStatus = "UNBLOCKED" // alias for compatibility
)

// RawAPIClient defines the endpoint methods required by ZombieManager.
type RawAPIClient interface {
	GetAuthenticatedUserID(ctx context.Context) (int64, error)
	V1DestroyBlock(ctx context.Context, targetUserID int64) (*http.Response, []byte, error)
	V1CreateBlock(ctx context.Context, targetUserID int64) (*http.Response, []byte, error)
	V1GetUser(ctx context.Context, targetUserID int64) (*http.Response, []byte, error)
	V2Unblock(ctx context.Context, sourceUserID, targetUserID int64) (*http.Response, []byte, error)
	V2Block(ctx context.Context, sourceUserID, targetUserID int64) (*http.Response, []byte, error)
	V2GetUser(ctx context.Context, targetUserID int64) (*http.Response, []byte, error)
}

// ZombieManager coordinates unblocking with a 3-tier ghost/zombie block recovery state machine.
type ZombieManager struct {
	client RawAPIClient
	v1Mu   sync.Mutex
}

// NewZombieManager constructs a ZombieManager.
func NewZombieManager(client RawAPIClient) *ZombieManager {
	return &ZombieManager{
		client: client,
	}
}

// CheckUserExists verifies whether a target user account is active, deleted, or suspended.
// Returns (exists, indeterminate).
func (z *ZombieManager) CheckUserExists(ctx context.Context, userID int64) (bool, bool) {
	// Step 1: Query v2 /2/users/:id
	resp, body, err := z.client.V2GetUser(ctx, userID)
	if err == nil && resp != nil {
		if resp.StatusCode == http.StatusOK {
			var v2Resp struct {
				Data *struct {
					ID string `json:"id"`
				} `json:"data"`
				Errors []struct {
					Detail string `json:"detail"`
					Title  string `json:"title"`
				} `json:"errors"`
			}
			if jsonErr := json.Unmarshal(body, &v2Resp); jsonErr == nil {
				if v2Resp.Data != nil && v2Resp.Data.ID != "" {
					return true, false
				}
				for _, apiErr := range v2Resp.Errors {
					detail := strings.ToLower(apiErr.Detail)
					title := strings.ToLower(apiErr.Title)
					if strings.Contains(detail, "could not find user with id") ||
						strings.Contains(title, "not found") {
						return false, false
					}
				}
			}
		} else if resp.StatusCode == http.StatusNotFound {
			return false, false
		}
	}

	logging.Debug(fmt.Sprintf("V2 existence check failed for %d. Falling back to V1.", userID))

	// Step 2: Query v1.1 /1.1/users/show.json
	z.v1Mu.Lock()
	v1Resp, _, v1Err := z.client.V1GetUser(ctx, userID)
	z.v1Mu.Unlock()

	if v1Err == nil && v1Resp != nil {
		if v1Resp.StatusCode == http.StatusOK {
			return true, false
		}
		if v1Resp.StatusCode == http.StatusNotFound {
			return false, false
		}
		if v1Resp.StatusCode == http.StatusForbidden {
			logging.Warnf("User %d is suspended (Forbidden). Treating as Ghost Block.", userID)
			return false, false
		}
	} else if v1Resp != nil {
		if v1Resp.StatusCode == http.StatusNotFound {
			return false, false
		}
		if v1Resp.StatusCode == http.StatusForbidden {
			logging.Warnf("User %d is suspended (Forbidden). Treating as Ghost Block.", userID)
			return false, false
		}
	}

	// Indeterminate: both checks experienced network/server errors
	return false, true
}

// UnblockUser executes the 3-Tier zombie unblock protocol matching Python x_service parity.
func (z *ZombieManager) UnblockUser(ctx context.Context, targetUserID int64) (UnblockStatus, error) {
	// =========================================================================
	// Tier 1: Twitter API v1.1 Direct Destroy Block
	// =========================================================================
	z.v1Mu.Lock()
	resp, _, err := z.client.V1DestroyBlock(ctx, targetUserID)
	z.v1Mu.Unlock()

	if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
		return StatusUnblocked, nil
	}

	is404 := false
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		is404 = true
	} else if err != nil && IsNotFound(err) {
		is404 = true
	}

	if !is404 {
		if err != nil {
			logging.Warnf("Failed to unblock %d: %v", targetUserID, err)
			return StatusFailed, err
		}
		logging.Warnf("Failed to unblock %d: HTTP %d", targetUserID, resp.StatusCode)
		return StatusFailed, fmt.Errorf("v1 unblock returned HTTP %d", resp.StatusCode)
	}

	// =========================================================================
	// Tier 1 returned 404: Verify User Existence
	// =========================================================================
	logging.Debug(fmt.Sprintf("User %d unblock failed with NotFound. Checking if user exists...", targetUserID))
	exists, indeterminate := z.CheckUserExists(ctx, targetUserID)
	if indeterminate {
		logging.Warnf("Could not verify existence of %d. Returning FAILED to retry later.", targetUserID)
		return StatusFailed, errors.New("user existence indeterminate")
	}
	if !exists {
		logging.Debug(fmt.Sprintf("User %d does not exist. Ignoring NotFound.", targetUserID))
		return StatusNotFound, nil
	}

	logging.Infof("User ID %d EXISTS. Attempting recovery strategies (Zombie Fix)...", targetUserID)

	// =========================================================================
	// Tier 2: Twitter API v2 Direct Unblock
	// =========================================================================
	srcUserID, err := z.client.GetAuthenticatedUserID(ctx)
	if err != nil {
		logging.Errorf("Failed to retrieve authenticated user ID for v2 unblock: %v", err)
		return StatusFailed, err
	}

	v2Resp, v2Body, v2Err := z.client.V2Unblock(ctx, srcUserID, targetUserID)
	if v2Err == nil && v2Resp != nil && v2Resp.StatusCode == http.StatusOK {
		logging.Infof("Zombie recovered via Tier 2 (v2 unblock) for user %d", targetUserID)
		return StatusUnblocked, nil
	}

	if v2Resp != nil {
		if strings.Contains(string(v2Body), "text/html") {
			logging.Warnf("V2 Unblock failed for %d: API returned HTML (likely 404/500) instead of JSON.", targetUserID)
		} else {
			logging.Warnf("V2 Unblock ALSO returned %d for %d", v2Resp.StatusCode, targetUserID)
		}
	} else {
		logging.Warnf("V2 Unblock failed for %d: %v", targetUserID, v2Err)
	}

	// =========================================================================
	// Tier 3a: Twitter API v1.1 Toggle Block (Create -> Destroy)
	// =========================================================================
	z.v1Mu.Lock()
	createResp, _, createErr := z.client.V1CreateBlock(ctx, targetUserID)
	var destroyResp *http.Response
	var destroyErr error
	if createErr == nil && createResp != nil && createResp.StatusCode == http.StatusOK {
		destroyResp, _, destroyErr = z.client.V1DestroyBlock(ctx, targetUserID)
	}
	z.v1Mu.Unlock()

	if createErr == nil && destroyErr == nil && destroyResp != nil && destroyResp.StatusCode == http.StatusOK {
		logging.Infof("Zombie recovered via Tier 3a (v1.1 toggle block) for user %d", targetUserID)
		return StatusUnblocked, nil
	}
	logging.Warnf("V1 Toggle Block Fix failed for %d: createErr=%v, destroyErr=%v", targetUserID, createErr, destroyErr)

	// =========================================================================
	// Tier 3b: Twitter API v2 Toggle Block (Create -> Destroy)
	// =========================================================================
	blockResp, _, blockErr := z.client.V2Block(ctx, srcUserID, targetUserID)
	var unblockResp *http.Response
	var unblockErr error
	if blockErr == nil && blockResp != nil && blockResp.StatusCode == http.StatusOK {
		unblockResp, _, unblockErr = z.client.V2Unblock(ctx, srcUserID, targetUserID)
	}

	if blockErr == nil && unblockErr == nil && unblockResp != nil && unblockResp.StatusCode == http.StatusOK {
		logging.Infof("Zombie recovered via Tier 3b (v2 toggle block) for user %d", targetUserID)
		return StatusUnblocked, nil
	}
	logging.Warnf("V2 Toggle Block Fix failed for %d: blockErr=%v, unblockErr=%v", targetUserID, blockErr, unblockErr)

	// =========================================================================
	// Terminal Exhaustion: True Zombie Block
	// =========================================================================
	logging.Warnf("All recovery strategies failed for %d (True Zombie Block). Skipping to avoid infinite retries.", targetUserID)
	return StatusNotFound, nil
}

// UnblockUser delegates directly to ZombieManager.
func (c *Client) UnblockUser(ctx context.Context, userID int64) (string, error) {
	status, err := c.zombie.UnblockUser(ctx, userID)
	return string(status), err
}
