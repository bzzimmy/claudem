package proxy

import (
	"encoding/json"

	"github.com/bzzimmy/claudem/internal/ccident"
)

// injectSystemPrefix ensures the Claude Code identity is the exact, complete
// first system block. The client's system prompt (string or blocks) follows it.
// Returns the body unchanged if it isn't a JSON object.
func injectSystemPrefix(body []byte) []byte {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil || doc == nil {
		return body
	}
	prefix := json.RawMessage(`{"type":"text","text":` + mustJSON(ccident.SystemPrefix) + `}`)

	var system []json.RawMessage
	if raw, ok := doc["system"]; ok && len(raw) > 0 && string(raw) != "null" {
		var s string
		switch {
		case json.Unmarshal(raw, &s) == nil:
			if s == ccident.SystemPrefix {
				return body
			}
			system = []json.RawMessage{prefix, json.RawMessage(`{"type":"text","text":` + mustJSON(s) + `}`)}
		case json.Unmarshal(raw, &system) == nil:
			if len(system) > 0 && isPrefixBlock(system[0]) {
				return body
			}
			system = append([]json.RawMessage{prefix}, system...)
		default:
			return body
		}
	} else {
		system = []json.RawMessage{prefix}
	}

	sys, err := json.Marshal(system)
	if err != nil {
		return body
	}
	doc["system"] = sys
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

func isPrefixBlock(raw json.RawMessage) bool {
	var b struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	return json.Unmarshal(raw, &b) == nil && b.Type == "text" && b.Text == ccident.SystemPrefix
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
