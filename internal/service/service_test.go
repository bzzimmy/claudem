package service

import (
	"encoding/xml"
	"strings"
	"testing"
)

var spec = Spec{
	Binary: "/usr/local/bin/claudem",
	Args:   []string{"-listen", "127.0.0.1:9000", "-rewrites", "/home/u/my rules.json"},
	Home:   "/home/u",
}

func TestRenderPlist(t *testing.T) {
	out, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal([]byte(out), new(struct{})); err != nil {
		t.Fatalf("plist is not well-formed XML: %v\n%s", err, out)
	}
	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>/usr/local/bin/claudem</string>",
		"<string>-listen</string>",
		"<string>127.0.0.1:9000</string>",
		"<string>/home/u/my rules.json</string>",
		"<string>/home/u/Library/Logs/claudem.log</string>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q:\n%s", want, out)
		}
	}
}

func TestRenderPlistEscapes(t *testing.T) {
	out, err := RenderPlist(Spec{Binary: "/b", Args: []string{`a<b>&"c"`}, Home: "/h"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<string>a&lt;b&gt;&amp;&#34;c&#34;</string>") {
		t.Fatalf("xml not escaped:\n%s", out)
	}
}

func TestRenderUnit(t *testing.T) {
	out, err := RenderUnit(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := `ExecStart=/usr/local/bin/claudem -listen 127.0.0.1:9000 -rewrites "/home/u/my rules.json"`
	if !strings.Contains(out, want) {
		t.Fatalf("ExecStart mismatch, want %q in:\n%s", want, out)
	}
	if !strings.Contains(out, "WantedBy=default.target") {
		t.Fatalf("missing install section:\n%s", out)
	}
}

func TestSystemdQuote(t *testing.T) {
	cases := map[string]string{
		"plain":       "plain",
		"has space":   `"has space"`,
		`q"uote`:      `"q\"uote"`,
		"$HOME":       `"$$HOME"`,
		"50%":         `"50%%"`,
		`back\slash`:  `"back\\slash"`,
		"":            `""`,
		"/a/b/c.json": "/a/b/c.json",
	}
	for in, want := range cases {
		if got := systemdQuote(in); got != want {
			t.Errorf("systemdQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
