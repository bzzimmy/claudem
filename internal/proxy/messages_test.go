package proxy

import (
	"encoding/json"
	"testing"

	"github.com/bzzimmy/claudem/internal/ccident"
)

func systemOf(t *testing.T, body []byte) []map[string]any {
	var doc struct {
		System []map[string]any `json:"system"`
		Model  string           `json:"model"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("bad output %s: %v", body, err)
	}
	if doc.Model != "m" {
		t.Fatalf("other fields lost: %s", body)
	}
	return doc.System
}

func TestInjectSystemPrefix(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []string
	}{
		"none":    {`{"model":"m"}`, []string{ccident.SystemPrefix}},
		"null":    {`{"model":"m","system":null}`, []string{ccident.SystemPrefix}},
		"string":  {`{"model":"m","system":"be terse"}`, []string{ccident.SystemPrefix, "be terse"}},
		"blocks":  {`{"model":"m","system":[{"type":"text","text":"a","cache_control":{"type":"ephemeral"}},{"type":"text","text":"b"}]}`, []string{ccident.SystemPrefix, "a", "b"}},
		"already": {`{"model":"m","system":[{"type":"text","text":"` + ccident.SystemPrefix + `"},{"type":"text","text":"x"}]}`, []string{ccident.SystemPrefix, "x"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sys := systemOf(t, injectSystemPrefix([]byte(tc.in)))
			if len(sys) != len(tc.want) {
				t.Fatalf("got %d blocks, want %d: %v", len(sys), len(tc.want), sys)
			}
			for i, w := range tc.want {
				if sys[i]["text"] != w || sys[i]["type"] != "text" {
					t.Fatalf("block %d = %v, want %q", i, sys[i], w)
				}
			}
			if name == "blocks" && sys[1]["cache_control"] == nil {
				t.Fatal("client cache_control dropped")
			}
		})
	}

	if s := `{"model":"m","system":"` + ccident.SystemPrefix + `"}`; string(injectSystemPrefix([]byte(s))) != s {
		t.Fatal("exact string prefix should pass through unchanged")
	}
	if out := injectSystemPrefix([]byte("not json")); string(out) != "not json" {
		t.Fatal("non-JSON should pass through")
	}
}

func TestMergeBetas(t *testing.T) {
	got := mergeBetas([]string{"a", "b"}, " b, c ,,a")
	if got != "a,b,c" {
		t.Fatalf("got %q", got)
	}
}
