package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bzzimmy/claudem/internal/ccident"
	"github.com/bzzimmy/claudem/internal/creds"
	"github.com/bzzimmy/claudem/internal/rewrite"
)

func TestForwardHeadersAndDiagnostic(t *testing.T) {
	var got *http.Request
	var gotBody []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Up", "1")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"`+thirdPartyMarker+`, not your plan limits."}}`)
	}))
	defer up.Close()

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	h, err := New(up.URL, creds.NewManager(nil, "sk-ant-oat01-test"), rewrite.New(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m","system":"hi","messages":[]}`))
	req.Header.Set("x-api-key", "client-key")
	req.Header.Set("anthropic-beta", "custom-beta")
	req.Header.Set("anthropic-version", "2023-06-01")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || rec.Header().Get("X-Up") != "1" {
		t.Fatalf("status/headers not passed through: %d %v", rec.Code, rec.Header())
	}
	if !strings.Contains(rec.Body.String(), thirdPartyMarker) {
		t.Fatalf("body not passed through: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "third-party harness fingerprint") {
		t.Fatalf("diagnostic not logged: %s", logs.String())
	}

	if got.Header.Get("X-Api-Key") != "" || got.Header.Get("Authorization") != "Bearer sk-ant-oat01-test" {
		t.Fatalf("auth headers: %v", got.Header)
	}
	if got.Header.Get("Anthropic-Beta") != "claude-code-20250219,oauth-2025-04-20,custom-beta" {
		t.Fatalf("beta merge: %q", got.Header.Get("Anthropic-Beta"))
	}
	if got.Header.Get("User-Agent") != ccident.UserAgent || got.Header.Get("X-App") != "cli" {
		t.Fatalf("identity headers: %v", got.Header)
	}
	if got.URL.Query().Get("beta") != "true" {
		t.Fatalf("beta query missing: %s", got.URL.String())
	}
	var body struct {
		System []struct{ Text string } `json:"system"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil || len(body.System) != 2 || body.System[0].Text != ccident.SystemPrefix || body.System[1].Text != "hi" {
		t.Fatalf("system rewrite: %s", gotBody)
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/v1/messages":              "/v1/messages",
		"/v1/v1/messages":           "/v1/messages",
		"/v1/v1/v1/messages":        "/v1/messages",
		"/anthropic/v1/messages":    "/v1/messages",
		"/anthropic/v1/v1/messages": "/v1/messages",
		"/anthropic":                "/",
		"/healthz":                  "/healthz",
		"/v1/models":                "/v1/models",
		"/messages":                 "/v1/messages",
		"/messages/count_tokens":    "/v1/messages/count_tokens",
		"/models":                   "/v1/models",
		"/models/claude-x":          "/v1/models/claude-x",
		"/anthropic/messages":       "/v1/messages",
		"/usage":                    "/usage",
		"/messagesx":                "/messagesx",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePathRoutesToMessages(t *testing.T) {
	var gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	h, err := New(up.URL, creds.NewManager(nil, "tok"), rewrite.New(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/v1/messages", strings.NewReader(`{"model":"m","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if gotPath != "/v1/messages" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if req.URL.Query().Get("beta") != "true" {
		t.Fatalf("messages route not taken: %s", req.URL.String())
	}
}
