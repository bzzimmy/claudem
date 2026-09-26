package creds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type memStore struct{ data []byte }

func (m *memStore) Load(context.Context) ([]byte, error)   { return m.data, nil }
func (m *memStore) Save(_ context.Context, b []byte) error { m.data = b; return nil }
func (m *memStore) String() string                         { return "mem" }

func TestTokenRefreshesAndWritesBack(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "sk-ant-oat01-new", "refresh_token": "sk-ant-ort01-new", "expires_in": 28800,
		})
	}))
	defer srv.Close()

	store := &memStore{data: []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-old","refreshToken":"sk-ant-ort01-old","expiresAt":1,"scopes":["user:inference"],"subscriptionType":"max"},"other":{"keep":true}}`)}
	m := NewManager(store, "")
	m.tokenURL = srv.URL

	tok, err := m.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "sk-ant-oat01-new" {
		t.Fatalf("token = %q", tok)
	}
	if got["grant_type"] != "refresh_token" || got["refresh_token"] != "sk-ant-ort01-old" || got["client_id"] == "" {
		t.Fatalf("refresh request = %v", got)
	}

	var doc map[string]json.RawMessage
	json.Unmarshal(store.data, &doc)
	if string(doc["other"]) != `{"keep":true}` {
		t.Fatalf("unknown top-level field lost: %s", store.data)
	}
	c, err := parse(store.data)
	if err != nil {
		t.Fatal(err)
	}
	if c.RefreshToken != "sk-ant-ort01-new" || c.SubscriptionType != "max" || c.ExpiresAt < time.Now().Add(7*time.Hour).UnixMilli() {
		t.Fatalf("written creds = %+v", c)
	}

	// Cached now; a second call must not hit the store again even if it changes.
	store.data = nil
	if tok, _ := m.Token(context.Background()); tok != "sk-ant-oat01-new" {
		t.Fatalf("cached token = %q", tok)
	}
}

func TestTokenPrefersFreshStoreOverRefresh(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	exp := time.Now().Add(time.Hour).UnixMilli()
	store := &memStore{data: []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-fresh","refreshToken":"r","expiresAt":` + itoa(exp) + `}}`)}
	m := NewManager(store, "")
	m.tokenURL = srv.URL
	if tok, err := m.Token(context.Background()); err != nil || tok != "sk-ant-oat01-fresh" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
	if called {
		t.Fatal("refresh endpoint called for a valid token")
	}
}

func TestParseFractionalExpiresAt(t *testing.T) {
	for _, in := range []string{`1790453738680.9412`, `1790453738680`, `1.7904537386809412e12`} {
		c, err := parse([]byte(`{"claudeAiOauth":{"accessToken":"a","refreshToken":"r","expiresAt":` + in + `}}`))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if c.ExpiresAt != 1790453738680 {
			t.Fatalf("%s: expiresAt=%d", in, c.ExpiresAt)
		}
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
