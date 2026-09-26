package creds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bzzimmy/claudem/internal/ccident"
)

// refreshBuffer: refresh this long before the access token expires.
const refreshBuffer = 5 * time.Minute

// Manager hands out a valid access token, refreshing and persisting as needed.
type Manager struct {
	store    Store
	static   string // CLAUDE_CODE_OAUTH_TOKEN / --token override; never refreshed
	client   *http.Client
	tokenURL string

	mu    sync.Mutex
	cur   Credentials
	valid bool
}

func NewManager(store Store, staticToken string) *Manager {
	return &Manager{
		store:    store,
		static:   staticToken,
		client:   &http.Client{Timeout: 30 * time.Second},
		tokenURL: ccident.OAuthRefreshURL,
	}
}

// Token returns a non-expired access token.
func (m *Manager) Token(ctx context.Context) (string, error) {
	if m.static != "" {
		return m.static, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.valid && !expiringSoon(m.cur) {
		return m.cur.AccessToken, nil
	}

	// Re-read first: Claude Code may already have refreshed and rotated.
	raw, err := m.store.Load(ctx)
	if err != nil {
		return "", err
	}
	c, err := parse(raw)
	if err != nil {
		return "", err
	}
	if !expiringSoon(c) {
		m.cur, m.valid = c, true
		return c.AccessToken, nil
	}

	if c.RefreshToken == "" {
		return "", fmt.Errorf("access token expired and no refresh token available")
	}
	nc, err := m.refresh(ctx, c)
	if err != nil {
		return "", err
	}
	merged, err := merge(raw, nc)
	if err != nil {
		return "", err
	}
	if err := m.store.Save(ctx, merged); err != nil {
		// Still usable for this process; warn loudly since Claude Code will be logged out.
		slog.Warn("token refreshed but write-back failed; Claude Code may be logged out", "store", m.store.String(), "err", err)
	} else {
		slog.Info("refreshed OAuth token", "expiresAt", time.UnixMilli(nc.ExpiresAt).Format(time.RFC3339))
	}
	m.cur, m.valid = nc, true
	return nc.AccessToken, nil
}

// Invalidate drops the cached token so the next call re-reads the store.
func (m *Manager) Invalidate() {
	m.mu.Lock()
	m.valid = false
	m.mu.Unlock()
}

// Info returns non-secret details for /healthz.
func (m *Manager) Info(ctx context.Context) (map[string]any, error) {
	if _, err := m.Token(ctx); err != nil {
		return nil, err
	}
	if m.static != "" {
		return map[string]any{"source": "static"}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"source":           m.store.String(),
		"subscriptionType": m.cur.SubscriptionType,
		"rateLimitTier":    m.cur.RateLimitTier,
		"expiresAt":        time.UnixMilli(m.cur.ExpiresAt).Format(time.RFC3339),
	}, nil
}

func expiringSoon(c Credentials) bool {
	return time.Now().Add(refreshBuffer).UnixMilli() >= c.ExpiresAt
}

func (m *Manager) refresh(ctx context.Context, c Credentials) (Credentials, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": c.RefreshToken,
		"client_id":     ccident.OAuthClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("token refresh: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("token refresh: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(data))
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &tr); err != nil || tr.AccessToken == "" {
		return Credentials{}, fmt.Errorf("token refresh: bad response")
	}
	nc := c
	nc.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		nc.RefreshToken = tr.RefreshToken
	}
	nc.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UnixMilli()
	return nc, nil
}
