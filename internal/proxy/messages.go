package proxy

import (
	"encoding/json"

	"github.com/bzzimmy/claudem/internal/ccident"
	"github.com/bzzimmy/claudem/internal/rewrite"
)

// prepareMessages rewrites a /v1/messages body for the OAuth path:
//   - harness fingerprint rewrites are applied to every system text block
//   - the Claude Code identity becomes the exact, complete first system block
//
// Returns the body unchanged if it isn't a JSON object, plus the names of
// harnesses whose rewrite rules matched.
func prepareMessages(body []byte, rw *rewrite.Rewriter) ([]byte, []string) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil || doc == nil {
		return body, nil
	}

	var blocks []json.RawMessage
	if raw, ok := doc["system"]; ok && len(raw) > 0 && string(raw) != "null" {
		var s string
		switch {
		case json.Unmarshal(raw, &s) == nil:
			blocks = []json.RawMessage{textBlock(s)}
		case json.Unmarshal(raw, &blocks) == nil:
		default:
			return body, nil
		}
	}

	var hit []string
	for i, b := range blocks {
		blocks[i], hit = rewriteBlock(b, rw, hit)
	}
	if len(blocks) == 0 || !isPrefixBlock(blocks[0]) {
		blocks = append([]json.RawMessage{textBlock(ccident.SystemPrefix)}, blocks...)
	}

	sys, err := json.Marshal(blocks)
	if err != nil {
		return body, nil
	}
	doc["system"] = sys
	out, err := json.Marshal(doc)
	if err != nil {
		return body, nil
	}
	return out, hit
}

// rewriteBlock applies rw to a text block, preserving its other fields
// (cache_control etc.). Non-text blocks pass through.
func rewriteBlock(raw json.RawMessage, rw *rewrite.Rewriter, hit []string) (json.RawMessage, []string) {
	var block map[string]json.RawMessage
	if json.Unmarshal(raw, &block) != nil || string(block["type"]) != `"text"` {
		return raw, hit
	}
	var text string
	if json.Unmarshal(block["text"], &text) != nil {
		return raw, hit
	}
	out, names := rw.Apply(text)
	if len(names) == 0 {
		return raw, hit
	}
	block["text"] = json.RawMessage(mustJSON(out))
	b, err := json.Marshal(block)
	if err != nil {
		return raw, hit
	}
	return b, append(hit, names...)
}

func textBlock(s string) json.RawMessage {
	return json.RawMessage(`{"type":"text","text":` + mustJSON(s) + `}`)
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
