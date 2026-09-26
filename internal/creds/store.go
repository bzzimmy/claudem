// Package creds loads Claude Code's OAuth credentials, refreshes them when
// expired, and writes rotated tokens back so Claude Code stays logged in.
package creds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const keychainService = "Claude Code-credentials"

// Store is where Claude Code keeps its credentials JSON.
type Store interface {
	Load(ctx context.Context) ([]byte, error)
	Save(ctx context.Context, data []byte) error
	String() string
}

// DefaultStore picks the Keychain on macOS, the credentials file elsewhere.
func DefaultStore() Store {
	if runtime.GOOS == "darwin" {
		return keychainStore{}
	}
	return fileStore{path: credentialsPath()}
}

func credentialsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

type keychainStore struct{}

func (keychainStore) String() string { return "keychain:" + keychainService }

func (keychainStore) Load(ctx context.Context) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", keychainService, "-w").Output()
	if err != nil {
		return nil, fmt.Errorf("keychain read: %w (is Claude Code logged in?)", err)
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

func (keychainStore) Save(ctx context.Context, data []byte) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	//nolint:gosec // fixed flags; data is the credentials JSON we just produced
	cmd := exec.CommandContext(ctx, "security", "add-generic-password", "-U",
		"-a", u.Username, "-s", keychainService, "-w", string(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain write: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

type fileStore struct{ path string }

func (f fileStore) String() string { return f.path }

func (f fileStore) Load(context.Context) ([]byte, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s not found (is Claude Code logged in?)", f.path)
	}
	return data, err
}

func (f fileStore) Save(_ context.Context, data []byte) error {
	return os.WriteFile(f.path, data, 0o600)
}

// Credentials is the claudeAiOauth object inside the store.
type Credentials struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresAt        int64  `json:"expiresAt"` // unix ms
	SubscriptionType string `json:"subscriptionType"`
	RateLimitTier    string `json:"rateLimitTier"`
}

// UnmarshalJSON tolerates a fractional expiresAt: recent Claude Code builds
// write the value as a float (e.g. 1790453738680.9412).
func (c *Credentials) UnmarshalJSON(b []byte) error {
	type plain Credentials
	var aux struct {
		*plain
		ExpiresAt json.Number `json:"expiresAt"`
	}
	aux.plain = (*plain)(c)
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	if aux.ExpiresAt == "" {
		return nil
	}
	if n, err := aux.ExpiresAt.Int64(); err == nil {
		c.ExpiresAt = n
		return nil
	}
	f, err := aux.ExpiresAt.Float64()
	if err != nil {
		return fmt.Errorf("expiresAt: %w", err)
	}
	c.ExpiresAt = int64(f)
	return nil
}

// parse extracts credentials while keeping the raw document so unknown fields
// survive a write-back.
func parse(raw []byte) (Credentials, error) {
	var doc struct {
		ClaudeAiOauth Credentials `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Credentials{}, fmt.Errorf("credentials JSON: %w", err)
	}
	if doc.ClaudeAiOauth.AccessToken == "" {
		return Credentials{}, errors.New("credentials missing claudeAiOauth.accessToken")
	}
	return doc.ClaudeAiOauth, nil
}

// merge writes the rotated token fields into the raw document.
func merge(raw []byte, c Credentials) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var oauth map[string]any
	if err := json.Unmarshal(doc["claudeAiOauth"], &oauth); err != nil {
		return nil, err
	}
	oauth["accessToken"] = c.AccessToken
	oauth["refreshToken"] = c.RefreshToken
	oauth["expiresAt"] = c.ExpiresAt
	b, err := json.Marshal(oauth)
	if err != nil {
		return nil, err
	}
	doc["claudeAiOauth"] = b
	return json.Marshal(doc)
}
