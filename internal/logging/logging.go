// Package logging provides a minimal slog.Handler for terminal output.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// Handler formats records as:
//
//	05:18:40 INFO  claudem listening addr=http://127.0.0.1:8787
type Handler struct {
	mu    *sync.Mutex
	w     io.Writer
	attrs []slog.Attr
}

// New returns a Handler writing to w.
func New(w io.Writer) *Handler {
	return &Handler{mu: &sync.Mutex{}, w: w}
}

func (h *Handler) Enabled(context.Context, slog.Level) bool { return true }

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-5s %s", r.Time.Format("15:04:05"), r.Level, r.Message)
	for _, a := range h.attrs {
		writeAttr(&b, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, a)
		return true
	})
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{mu: h.mu, w: h.w, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *Handler) WithGroup(string) slog.Handler { return h }

func writeAttr(b *strings.Builder, a slog.Attr) {
	v := a.Value.Resolve()
	if a.Key == "" || v.Equal(slog.Value{}) {
		return
	}
	s := v.String()
	if list, ok := v.Any().([]string); ok {
		s = strings.Join(list, ",")
	}
	if strings.ContainsAny(s, " \t\n\"=") {
		s = strconv.Quote(s)
	}
	fmt.Fprintf(b, " %s=%s", a.Key, s)
}
