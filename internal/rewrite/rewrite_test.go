package rewrite

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestApply(t *testing.T) {
	r := New(Defaults, []Harness{{Name: "x", Rules: []Rule{{From: "foo", To: "bar"}}}})

	out, hit := r.Apply("read pi .md files and pi packages; foo")
	if out != "read cli .md files and cli packages; bar" {
		t.Fatalf("out = %q", out)
	}
	if !reflect.DeepEqual(hit, []string{"pi", "x"}) {
		t.Fatalf("hit = %v", hit)
	}

	out, hit = r.Apply("nothing to see")
	if out != "nothing to see" || hit != nil {
		t.Fatalf("unchanged text altered: %q %v", out, hit)
	}
}

func TestLoadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.json")
	os.WriteFile(p, []byte(`[{"name":"h","rules":[{"from":"a","to":"b"}]}]`), 0o600)
	hs, err := LoadFile(p)
	if err != nil || len(hs) != 1 || hs[0].Rules[0].From != "a" {
		t.Fatalf("hs=%v err=%v", hs, err)
	}

	os.WriteFile(p, []byte(`[{"name":"h","rules":[{"from":"","to":"b"}]}]`), 0o600)
	if _, err := LoadFile(p); err == nil {
		t.Fatal("empty from accepted")
	}
	os.WriteFile(p, []byte(`[{"rules":[]}]`), 0o600)
	if _, err := LoadFile(p); err == nil {
		t.Fatal("missing name accepted")
	}
}
