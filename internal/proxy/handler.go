// Package proxy is a transparent Anthropic Messages API proxy that swaps in
// Claude Code OAuth credentials and identity.
package proxy

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bzzimmy/claudem/internal/ccident"
	"github.com/bzzimmy/claudem/internal/creds"
	"github.com/bzzimmy/claudem/internal/rewrite"
)

// thirdPartyMarker is the upstream error text when the system prompt matched a
// known third-party harness fingerprint.
const thirdPartyMarker = "Third-party apps now draw from your extra usage"

const maxBody = 64 << 20

type Handler struct {
	upstream  *url.URL
	tokens    *creds.Manager
	rewriter  *rewrite.Rewriter
	betas     []string
	sessionID string
	client    *http.Client
	verbose   bool
}

func New(upstream string, tokens *creds.Manager, rw *rewrite.Rewriter, fullBetas, verbose bool) (*Handler, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	betas := ccident.BaseBetas
	if fullBetas {
		betas = ccident.FullBetas
	}
	return &Handler{
		upstream:  u,
		tokens:    tokens,
		rewriter:  rw,
		betas:     betas,
		sessionID: newUUID(),
		verbose:   verbose,
		client: &http.Client{
			Transport:     &http.Transport{DisableCompression: true, ResponseHeaderTimeout: 10 * time.Minute},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = normalizePath(r.URL.Path)
	switch {
	case r.URL.Path == "/healthz":
		h.healthz(w, r)
	case r.URL.Path == "/usage":
		r.URL.Path = "/api/oauth/usage"
		h.forward(w, r, nil)
	case strings.HasPrefix(r.URL.Path, "/v1/messages/batches"):
		writeErr(w, http.StatusNotImplemented, "not_supported_error", "message batches are not available on subscription OAuth")
	case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		q := r.URL.Query()
		q.Set("beta", "true")
		r.URL.RawQuery = q.Encode()
		body, hit := prepareMessages(body, h.rewriter)
		if h.verbose && len(hit) > 0 {
			slog.Info("rewrote system prompt", "harness", hit)
		}
		h.forward(w, r, body)
	default:
		h.forward(w, r, nil)
	}
}

// normalizePath tolerates common client misconfigurations: a base URL that
// already ends in /v1 (yielding /v1/v1/...), one that is missing /v1 for
// clients that expect it included (yielding /messages), and an /anthropic
// prefix some multi-provider clients add.
func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "/anthropic")
	for strings.HasPrefix(p, "/v1/v1/") {
		p = p[len("/v1"):]
	}
	for _, root := range []string{"/messages", "/models", "/complete"} {
		if p == root || strings.HasPrefix(p, root+"/") {
			p = "/v1" + p
			break
		}
	}
	if p == "" {
		p = "/"
	}
	return p
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	info, err := h.tokens.Info(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "credentials_error", err.Error())
		return
	}
	info["ok"] = true
	writeJSON(w, http.StatusOK, info)
}

// forward sends the request upstream with Claude Code auth + identity and
// streams the response back untouched. body==nil streams the client's body.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, body []byte) {
	token, err := h.tokens.Token(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "credentials_error", err.Error())
		return
	}

	u := *h.upstream
	u.Path, u.RawQuery = r.URL.Path, r.URL.RawQuery
	var rd io.Reader = r.Body
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), rd)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	} else {
		req.ContentLength = r.ContentLength
	}
	h.setHeaders(req.Header, r.Header, token)

	start := time.Now()
	resp, err := h.client.Do(req) //nolint:gosec // reverse proxy: host is fixed, only the path comes from the client
	if err != nil {
		writeErr(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		h.tokens.Invalidate()
	}

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	var src io.Reader = resp.Body
	if resp.StatusCode == http.StatusBadRequest {
		src = h.diagnoseBadRequest(resp.Body)
	}
	n, _ := copyFlush(w, src)
	if h.verbose || resp.StatusCode >= 400 {
		slog.Info("proxied", "method", r.Method, "path", r.URL.Path, "status", resp.StatusCode, "bytes", n, "dur", time.Since(start).Round(time.Millisecond))
	}
}

// diagnoseBadRequest peeks at a 400 body and logs an actionable hint when the
// system prompt was fingerprinted as a third-party harness.
func (h *Handler) diagnoseBadRequest(body io.Reader) io.Reader {
	peek, _ := io.ReadAll(io.LimitReader(body, 4096))
	if bytes.Contains(peek, []byte(thirdPartyMarker)) {
		slog.Warn("system prompt matched Anthropic's third-party harness fingerprint; " +
			"add a rewrite rule for this client (see --rewrites)")
	}
	return io.MultiReader(bytes.NewReader(peek), body)
}

func (h *Handler) setHeaders(dst, src http.Header, token string) {
	for k, v := range src {
		switch strings.ToLower(k) {
		case "x-api-key", "authorization", "host", "content-length", "connection",
			"anthropic-beta", "user-agent", "x-app", "accept":
			continue
		}
		if strings.HasPrefix(strings.ToLower(k), "x-stainless-") {
			continue
		}
		dst[k] = v
	}
	for k, v := range ccident.Headers() {
		dst.Set(k, v)
	}
	dst.Set("Authorization", "Bearer "+token)
	dst.Set("X-Claude-Code-Session-Id", h.sessionID)
	if dst.Get("Anthropic-Version") == "" {
		dst.Set("Anthropic-Version", ccident.APIVersion)
	}
	dst.Set("Anthropic-Beta", mergeBetas(h.betas, src.Get("Anthropic-Beta")))
}

func mergeBetas(base []string, client string) string {
	seen := map[string]bool{}
	var out []string
	add := func(b string) {
		b = strings.TrimSpace(b)
		if b != "" && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	for _, b := range base {
		add(b)
	}
	for _, b := range strings.Split(client, ",") {
		add(b)
	}
	return strings.Join(out, ",")
}

func copyFlush(w http.ResponseWriter, r io.Reader) (int64, error) {
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

func writeErr(w http.ResponseWriter, status int, typ, msg string) {
	slog.Warn("error", "status", status, "type", typ, "msg", msg)
	writeJSON(w, status, map[string]any{
		"type":  "error",
		"error": map[string]string{"type": typ, "message": msg},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
