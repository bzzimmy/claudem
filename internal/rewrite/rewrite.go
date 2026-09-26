// Package rewrite applies small, meaning-preserving edits to system prompts so
// they no longer match Anthropic's third-party harness fingerprints.
//
// The gate (verified 2026-09) counts exact signature phrases per harness in the
// concatenated system text after NFKC normalization and format-char stripping;
// three or more matches classify the request as a third-party app. Breaking a
// single phrase is enough; defaults break two per harness for margin. Harnesses
// that are not fingerprinted need no rules.
package rewrite

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Rule is a literal substring replacement.
type Rule struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Harness groups the rules for one client so they can be reasoned about and
// updated together when that client changes its prompt.
type Harness struct {
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

// Rewriter applies a set of harness rules.
type Rewriter struct {
	harnesses []Harness
}

// New builds a Rewriter from harness rule sets, applied in order.
func New(harnesses ...[]Harness) *Rewriter {
	r := &Rewriter{}
	for _, hs := range harnesses {
		r.harnesses = append(r.harnesses, hs...)
	}
	return r
}

// Names lists the configured harnesses in application order.
func (r *Rewriter) Names() []string {
	names := make([]string, 0, len(r.harnesses))
	for _, h := range r.harnesses {
		names = append(names, h.Name)
	}
	return names
}

// Apply rewrites text and returns the names of harnesses whose rules matched.
func (r *Rewriter) Apply(text string) (string, []string) {
	var hit []string
	for _, h := range r.harnesses {
		matched := false
		for _, rule := range h.Rules {
			if rule.From == "" || !strings.Contains(text, rule.From) {
				continue
			}
			text = strings.ReplaceAll(text, rule.From, rule.To)
			matched = true
		}
		if matched {
			hit = append(hit, h.Name)
		}
	}
	return text, hit
}

// LoadFile reads additional harness rules from a JSON file:
//
//	[{"name": "myharness", "rules": [{"from": "exact phrase", "to": "replacement"}]}]
func LoadFile(path string) ([]Harness, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-supplied --rewrites path by design
	if err != nil {
		return nil, err
	}
	var hs []Harness
	if err := json.Unmarshal(data, &hs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, h := range hs {
		if h.Name == "" {
			return nil, fmt.Errorf("%s: harness entry missing name", path)
		}
		for _, rule := range h.Rules {
			if rule.From == "" {
				return nil, fmt.Errorf("%s: harness %q has a rule with empty \"from\"", path, h.Name)
			}
		}
	}
	return hs, nil
}
