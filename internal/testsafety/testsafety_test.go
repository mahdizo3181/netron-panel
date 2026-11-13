package testsafety

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The guard must answer instead of the host: a shadowed command runs only its
// stub, a download never leaves the machine, and each attempt is recorded in order.
func TestGuardRecordsInsteadOfRunning(t *testing.T) {
	g, err := install()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.ln.Close(); _ = os.RemoveAll(g.dir) })

	// Checked before anything runs: a broken guard must not reach the real systemd-run.
	for _, name := range []string{"systemd-run", "bash", "x-ui"} {
		path, err := exec.LookPath(name)
		if err != nil || !strings.HasPrefix(path, g.dir) {
			t.Fatalf("%s resolves to %q (%v), not to the guard's stub under %s", name, path, err, g.dir)
		}
	}
	if !strings.HasPrefix(os.Getenv("HTTPS_PROXY"), "http://127.0.0.1:") {
		t.Fatalf("HTTPS_PROXY = %q, not the guard's listener", os.Getenv("HTTPS_PROXY"))
	}
	if entries, err := os.ReadDir(os.Getenv("XUI_BIN_FOLDER")); err != nil || len(entries) != 0 ||
		!strings.HasPrefix(os.Getenv("XUI_BIN_FOLDER"), g.dir) {
		t.Fatalf("XUI_BIN_FOLDER = %q (%d entries, %v), want an empty guard folder", os.Getenv("XUI_BIN_FOLDER"), len(entries), err)
	}

	err = exec.CommandContext(context.Background(), "systemd-run", "--unit", "x-ui-web-update-1", "/usr/bin/true").Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 97 {
		t.Fatalf("systemd-run = %v, want the stub's exit 97", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://raw.githubusercontent.com/o/r/main/update.sh", nil)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("a download through the guard succeeded with %s", resp.Status)
	}
	g.Record("panel restart requested")

	want := []string{
		"exec: systemd-run --unit x-ui-web-update-1 /usr/bin/true",
		"network: CONNECT raw.githubusercontent.com:443 HTTP/1.1",
		"panel restart requested",
	}
	if got := g.Events(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := os.Stat(filepath.Join(g.dir, "exec.log")); err != nil {
		t.Fatalf("exec log missing: %v", err)
	}
}
