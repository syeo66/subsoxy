// Package audiomuse talks to an AudioMuse-AI server's own API (not the Navidrome plugin).
// Its smart fade endpoint puts a list of songs in an order where each one flows into the next.
package audiomuse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// The order is computed while a client waits for getRandomSongs, so a slow AudioMuse
	// gives up quickly. AudioMuse-AI documents 0.08 s for 101 songs.
	RequestTimeout = 3 * time.Second
	// After a failure no request is made for this long, so a server that is down or
	// unreachable doesn't add RequestTimeout to every shuffle
	FailureBackoff = 30 * time.Second
	// Largest list sent for ordering. Longer lists are left in their shuffled order.
	MaxSongs = 200

	smartFadePath   = "/api/recording_search/smart_fade"
	maxResponseSize = 4 << 20
)

// Client calls AudioMuse-AI's smart fade API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	logger  *logrus.Logger

	mu          sync.Mutex
	retryAfter  time.Time // No requests before this time, after a failure
	now         func() time.Time
	backoffTime time.Duration
}

// New returns a client for the AudioMuse-AI server at baseURL. The token is the server's API
// token, sent as a Bearer token, and may be empty for a server that needs none.
func New(baseURL, token string, logger *logrus.Logger) *Client {
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		token:       token,
		http:        &http.Client{Timeout: RequestTimeout},
		logger:      logger,
		now:         time.Now,
		backoffTime: FailureBackoff,
	}
}

type smartFadeRequest struct {
	ItemIDs []string `json:"item_ids"`
}

type smartFadeResponse struct {
	Results []struct {
		ItemID string `json:"item_id"`
	} `json:"results"`
}

// OrderSongs returns ids re-ordered so each song flows into the next. AudioMuse-AI documents
// that it keeps the first song first, but this isn't checked here. The result always holds
// exactly the given ids: ids AudioMuse doesn't return are appended in their original order,
// and ids it invents or repeats are dropped. A list that is too short or too long to order
// is returned as it is.
//
// An error means the order is unknown, and the caller should keep its own. After one, the
// next calls fail at once for FailureBackoff.
func (c *Client) OrderSongs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) < 3 || len(ids) > MaxSongs {
		// With two songs the first is fixed and the second has nowhere else to go
		return ids, nil
	}

	c.mu.Lock()
	if c.now().Before(c.retryAfter) {
		c.mu.Unlock()
		return nil, fmt.Errorf("audiomuse: backing off after an earlier failure")
	}
	c.mu.Unlock()

	ordered, err := c.smartFade(ctx, ids)
	if err != nil {
		// A client that gave up says nothing about AudioMuse
		if ctx.Err() == nil {
			c.mu.Lock()
			c.retryAfter = c.now().Add(c.backoffTime)
			c.mu.Unlock()
		}
		return nil, err
	}
	return mergeOrder(ids, ordered), nil
}

func (c *Client) smartFade(ctx context.Context, ids []string) ([]string, error) {
	body, err := json.Marshal(smartFadeRequest{ItemIDs: ids})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+smartFadePath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseSize))
		return nil, fmt.Errorf("audiomuse: smart_fade answered HTTP %d", resp.StatusCode)
	}

	var decoded smartFadeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("audiomuse: unreadable smart_fade response: %w", err)
	}

	ordered := make([]string, 0, len(decoded.Results))
	for _, r := range decoded.Results {
		ordered = append(ordered, r.ItemID)
	}
	return ordered, nil
}

// mergeOrder returns the ids in the order AudioMuse gave. Anything it left out goes last in
// its original order, and anything it added or repeated is ignored, so no song is lost or
// invented. An empty answer leaves the original order.
func mergeOrder(original, ordered []string) []string {
	known := make(map[string]bool, len(original))
	for _, id := range original {
		known[id] = true
	}

	result := make([]string, 0, len(original))
	placed := make(map[string]bool, len(original))
	for _, id := range ordered {
		if known[id] && !placed[id] {
			placed[id] = true
			result = append(result, id)
		}
	}
	for _, id := range original {
		if !placed[id] {
			placed[id] = true
			result = append(result, id)
		}
	}
	return result
}
