package proxy

import (
	"encoding/json"
	"testing"

	"github.com/bzzimmy/claudem/internal/ccident"
	"github.com/bzzimmy/claudem/internal/rewrite"
)

var noRules = rewrite.New()

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

func TestPrepareMessagesPrefix(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []string
	}{
		"none":    {`{"model":"m"}`, []string{ccident.SystemPrefix}},
		"null":    {`{"model":"m","system":null}`, []string{ccident.SystemPrefix}},
		"string":  {`{"model":"m","system":"be terse"}`, []string{ccident.SystemPrefix, "be terse"}},
		"blocks":  {`{"model":"m","system":[{"type":"text","text":"a","cache_control":{"type":"ephemeral"}},{"type":"text","text":"b"}]}`, []string{ccident.SystemPrefix, "a", "b"}},
		"already": {`{"model":"m","system":[{"type":"text","text":"` + ccident.SystemPrefix + `"},{"type":"text","text":"x"}]}`, []string{ccident.SystemPrefix, "x"}},
		"exact":   {`{"model":"m","system":"` + ccident.SystemPrefix + `"}`, []string{ccident.SystemPrefix}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, hit := prepareMessages([]byte(tc.in), noRules)
			if hit != nil {
				t.Fatalf("unexpected rewrite hits %v", hit)
			}
			sys := systemOf(t, out)
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

	if out, _ := prepareMessages([]byte("not json"), noRules); string(out) != "not json" {
		t.Fatal("non-JSON should pass through")
	}
}

func TestPrepareMessagesRewrites(t *testing.T) {
	rw := rewrite.New(rewrite.Defaults)
	in := `{"model":"m","system":[{"type":"text","text":"read pi packages","cache_control":{"type":"ephemeral"}},{"type":"text","text":"Workspace root folder: /x"},{"type":"text","text":"untouched"}]}`
	out, hit := prepareMessages([]byte(in), rw)
	if len(hit) != 2 || hit[0] != "pi" || hit[1] != "opencode" {
		t.Fatalf("hit = %v", hit)
	}
	sys := systemOf(t, out)
	want := []string{ccident.SystemPrefix, "read cli packages", "Workspace root: /x", "untouched"}
	for i, w := range want {
		if sys[i]["text"] != w {
			t.Fatalf("block %d = %q, want %q", i, sys[i]["text"], w)
		}
	}
	if sys[1]["cache_control"] == nil {
		t.Fatal("cache_control dropped on rewritten block")
	}

	// String system is rewritten too.
	out, hit = prepareMessages([]byte(`{"model":"m","system":"about pi itself"}`), rw)
	if len(hit) != 1 || systemOf(t, out)[1]["text"] != "about the cli itself" {
		t.Fatalf("string system: hit=%v out=%s", hit, out)
	}
}

func TestMergeBetas(t *testing.T) {
	got := mergeBetas([]string{"a", "b"}, " b, c ,,a")
	if got != "a,b,c" {
		t.Fatalf("got %q", got)
	}
}
