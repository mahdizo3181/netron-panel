// Package testsafety keeps a test binary off the host: recording stubs for system
// commands, no panel binaries, refused proxy-aware HTTP, and any attempt fails the test.
package testsafety

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// shadowed are the commands the panel starts by name (exec.LookPath / PATH).
var shadowed = []string{
	"bash", "sh", "systemd-run", "systemctl", "journalctl", "sudo", "pkexec", "su",
	"fail2ban-client", "pg_dump", "pg_restore", "ip", "nft", "iptables", "ip6tables",
	"modprobe", "awg", "awg-quick", "wg", "wg-quick", "curl", "wget", "x-ui",
}

// Guard records every blocked side effect of one test binary.
type Guard struct {
	dir      string
	execLog  string
	ln       net.Listener
	mu       sync.Mutex
	events   []string
	execSeen int
}

var active *Guard

// Main installs the guard, runs the tests and fails the binary on any blocked side
// effect, including ones from goroutines that outlive their test. Use it from TestMain.
func Main(m *testing.M, setup func(*Guard)) {
	g, err := install()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testsafety:", err)
		os.Exit(2)
	}
	active = g
	if setup != nil {
		setup(g)
	}
	code := m.Run()
	if events := g.Events(); len(events) > 0 {
		fmt.Fprintln(os.Stderr, "testsafety: the tests tried to touch the host; every attempt below was blocked:")
		for _, e := range events {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		code = 1
	}
	_ = g.ln.Close()
	_ = os.RemoveAll(g.dir)
	os.Exit(code)
}

func install() (*Guard, error) {
	dir, err := os.MkdirTemp("", "xui-testsafety-")
	if err != nil {
		return nil, err
	}
	g := &Guard{dir: dir, execLog: filepath.Join(dir, "exec.log")}
	pathDir, binDir := filepath.Join(dir, "path"), filepath.Join(dir, "bin")
	for _, d := range []string{pathDir, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	// The stub's own interpreter is an absolute path, so shadowing sh does not loop.
	stub := "#!/bin/sh\nprintf 'exec: %s %s\\n' \"$(basename \"$0\")\" \"$*\" >> '" + g.execLog + "'\nexit 97\n"
	for _, name := range shadowed {
		if err := os.WriteFile(filepath.Join(pathDir, name), []byte(stub), 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return nil, err
	}
	// An empty bin folder: Xray and mtg cannot start; tests that need a fake set their own.
	if os.Getenv("XUI_BIN_FOLDER") == "" {
		if err := os.Setenv("XUI_BIN_FOLDER", binDir); err != nil {
			return nil, err
		}
	}
	if g.ln, err = (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0"); err != nil {
		return nil, err
	}
	go g.refuse()
	proxy := "http://" + g.ln.Addr().String()
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		_ = os.Setenv(k, proxy)
	}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		_ = os.Setenv(k, "127.0.0.1,localhost,::1")
	}
	return g, nil
}

// refuse answers every proxied request with 403 after recording its request line.
func (g *Guard) refuse() {
	for {
		c, err := g.ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			line, _ := bufio.NewReader(c).ReadString('\n')
			g.Record("network: " + strings.TrimSpace(line))
			_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		}(c)
	}
}

// Record adds a blocked side effect, e.g. from a restart hook the test installed.
func (g *Guard) Record(event string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.collectExecLocked()
	g.events = append(g.events, event)
}

// Events lists every blocked side effect so far, in the order they were seen.
func (g *Guard) Events() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.collectExecLocked()
	return append([]string(nil), g.events...)
}

func (g *Guard) collectExecLocked() {
	raw, err := os.ReadFile(g.execLog)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, l := range lines[min(g.execSeen, len(lines)):] {
		if l != "" {
			g.events = append(g.events, l)
		}
	}
	g.execSeen = len(lines)
}

// Watch fails t for every side effect blocked while it ran; a no-op without Main.
func Watch(t testing.TB) {
	t.Helper()
	g := active
	if g == nil {
		return
	}
	before := len(g.Events())
	t.Cleanup(func() {
		for _, e := range g.Events()[before:] {
			t.Errorf("testsafety blocked a host side effect: %s", e)
		}
	})
}
