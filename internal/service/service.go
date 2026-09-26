// Package service installs claudem as a per-user background service: a
// launchd LaunchAgent on macOS, a systemd user unit on Linux. Per-user is
// required on macOS so the keychain of the logged-in session is reachable.
package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

const (
	Label = "com.bzzimmy.claudem"
	unit  = "claudem"
)

// Spec describes the service to install.
type Spec struct {
	Binary string   // absolute path to the claudem executable
	Args   []string // flags passed to claudem
	Home   string   // user home directory
}

// Result reports what Install/Uninstall touched so the CLI can print it.
type Result struct {
	Path string // unit file path
	Logs string // where stdout/stderr go
}

// Install writes the unit file and starts the service.
func Install(ctx context.Context, s Spec) (Result, error) {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(ctx, s)
	case "linux":
		return installSystemd(ctx, s)
	default:
		return Result{}, fmt.Errorf("service install is not supported on %s", runtime.GOOS)
	}
}

// Uninstall stops the service and removes the unit file.
func Uninstall(ctx context.Context, home string) (Result, error) {
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchd(ctx, home)
	case "linux":
		return uninstallSystemd(ctx, home)
	default:
		return Result{}, fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
	}
}

// --- launchd ---

func plistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

func launchdLog(home string) string {
	return filepath.Join(home, "Library", "Logs", "claudem.log")
}

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{"xml": xmlEscape}).Parse(
	`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>
	<key>ProgramArguments</key>
	<array>
{{- range .Program}}
		<string>{{xml .}}</string>
{{- end}}
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>{{xml .Log}}</string>
	<key>StandardErrorPath</key>
	<string>{{xml .Log}}</string>
</dict>
</plist>
`))

// RenderPlist returns the LaunchAgent plist for s.
func RenderPlist(s Spec) (string, error) {
	var b strings.Builder
	err := plistTmpl.Execute(&b, struct {
		Label   string
		Program []string
		Log     string
	}{Label, append([]string{s.Binary}, s.Args...), launchdLog(s.Home)})
	return b.String(), err
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func installLaunchd(ctx context.Context, s Spec) (Result, error) {
	res := Result{Path: plistPath(s.Home), Logs: launchdLog(s.Home)}
	content, err := RenderPlist(s)
	if err != nil {
		return res, err
	}
	if err := writeUnit(res.Path, content); err != nil {
		return res, err
	}
	if err := os.MkdirAll(filepath.Dir(res.Logs), 0o750); err != nil {
		return res, err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// bootout is best-effort: it fails when the agent is not loaded yet.
	_ = run(ctx, "launchctl", "bootout", domain, res.Path)
	if err := run(ctx, "launchctl", "bootstrap", domain, res.Path); err != nil {
		return res, err
	}
	return res, nil
}

func uninstallLaunchd(ctx context.Context, home string) (Result, error) {
	res := Result{Path: plistPath(home), Logs: launchdLog(home)}
	_ = run(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), res.Path)
	return res, removeUnit(res.Path)
}

// --- systemd ---

func unitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", unit+".service")
}

var unitTmpl = template.Must(template.New("unit").Funcs(template.FuncMap{"q": systemdQuote}).Parse(
	`[Unit]
Description=claudem - local Anthropic API proxy reusing Claude Code credentials
After=network-online.target

[Service]
ExecStart={{range $i, $a := .Program}}{{if $i}} {{end}}{{q $a}}{{end}}
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`))

// RenderUnit returns the systemd user unit for s.
func RenderUnit(s Spec) (string, error) {
	var b strings.Builder
	err := unitTmpl.Execute(&b, struct{ Program []string }{append([]string{s.Binary}, s.Args...)})
	return b.String(), err
}

// systemdQuote quotes an ExecStart argument per systemd.syntax(7).
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$%;") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `$$`, `%`, `%%`)
	return `"` + r.Replace(s) + `"`
}

func installSystemd(ctx context.Context, s Spec) (Result, error) {
	res := Result{Path: unitPath(s.Home), Logs: "journalctl --user -u " + unit}
	content, err := RenderUnit(s)
	if err != nil {
		return res, err
	}
	if err := writeUnit(res.Path, content); err != nil {
		return res, err
	}
	if err := run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return res, err
	}
	if err := run(ctx, "systemctl", "--user", "enable", "--now", unit); err != nil {
		return res, err
	}
	return res, run(ctx, "systemctl", "--user", "restart", unit)
}

func uninstallSystemd(ctx context.Context, home string) (Result, error) {
	res := Result{Path: unitPath(home), Logs: "journalctl --user -u " + unit}
	_ = run(ctx, "systemctl", "--user", "disable", "--now", unit)
	if err := removeUnit(res.Path); err != nil {
		return res, err
	}
	return res, run(ctx, "systemctl", "--user", "daemon-reload")
}

// --- shared ---

func writeUnit(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec // unit files are conventionally world-readable
}

func removeUnit(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	//nolint:gosec // name is always launchctl/systemctl; args are paths we generated
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
