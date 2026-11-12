package panel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// fakeGitHub serves this repo's releases API from respond and counts every request.
func fakeGitHub(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *atomic.Int32 {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	prevBase := githubAPIBase
	githubAPIBase = srv.URL
	resetUpdateInfoCache()
	t.Cleanup(func() {
		githubAPIBase = prevBase
		resetUpdateInfoCache()
	})
	return &hits
}

func serveLatest(tag string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+panelRepo+"/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	}
}

// nextRevisionTag returns a tag `ahead` netron revisions past the running version.
func nextRevisionTag(t *testing.T, ahead int) string {
	t.Helper()
	base, rev, ok := strings.Cut(config.GetBaseVersion(), "-netron.")
	n, err := strconv.Atoi(rev)
	if !ok || err != nil {
		t.Fatalf("running version %q is not a netron version", config.GetBaseVersion())
	}
	return fmt.Sprintf("v%s-netron.%d", base, n+ahead)
}

func TestGetUpdateInfoComparesAgainstLatestRelease(t *testing.T) {
	current := config.GetBaseVersion()
	cases := []struct {
		name, latest string
		want         bool
	}{
		{"next revision", nextRevisionTag(t, 1), true},
		// .10 after .5 orders numerically, not as the string "10" < "5".
		{"two-digit revision", nextRevisionTag(t, 10-mustRevision(t)), true},
		{"same version", "v" + current, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeGitHub(t, serveLatest(tc.latest))
			got, err := (&PanelService{}).GetUpdateInfo()
			if err != nil {
				t.Fatalf("GetUpdateInfo: %v", err)
			}
			want := PanelUpdateInfo{Channel: "stable", CurrentVersion: current, LatestVersion: tc.latest, UpdateAvailable: tc.want}
			if *got != want {
				t.Fatalf("GetUpdateInfo = %+v, want %+v", *got, want)
			}
		})
	}
}

func mustRevision(t *testing.T) int {
	t.Helper()
	_, rev, _ := strings.Cut(config.GetBaseVersion(), "-netron.")
	n, err := strconv.Atoi(rev)
	if err != nil {
		t.Fatalf("running version %q is not a netron version", config.GetBaseVersion())
	}
	if n >= 10 {
		t.Skipf("running revision %d already has two digits", n)
	}
	return n
}

// This fork publishes no dev-latest release, so a saved dev-channel switch
// must fall back to the stable check instead of failing on a 404 forever.
func TestGetUpdateInfoIgnoresDevChannelOnFork(t *testing.T) {
	latest := nextRevisionTag(t, 1)
	fakeGitHub(t, serveLatest(latest))
	if err := (&service.SettingService{}).SetDevChannelEnable(true); err != nil {
		t.Fatal(err)
	}
	got, err := (&PanelService{}).GetUpdateInfo()
	if err != nil {
		t.Fatalf("GetUpdateInfo with the dev switch on: %v", err)
	}
	if got.Channel != "stable" || got.LatestVersion != latest || !got.UpdateAvailable {
		t.Fatalf("GetUpdateInfo = %+v, want the stable %s update", *got, latest)
	}
}

func TestGetUpdateInfoReportsGitHubRefusal(t *testing.T) {
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded for 203.0.113.7."}`)
	})
	_, err := (&PanelService{}).GetUpdateInfo()
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "API rate limit exceeded") {
		t.Fatalf("GetUpdateInfo error = %v, want the 403 status and GitHub's message", err)
	}
}

func TestGetUpdateInfoCachesWithinTTL(t *testing.T) {
	hits := fakeGitHub(t, serveLatest(nextRevisionTag(t, 1)))
	for range 3 {
		if _, err := (&PanelService{}).GetUpdateInfo(); err != nil {
			t.Fatal(err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("GitHub requests for 3 checks within the TTL = %d, want 1", got)
	}
}

func TestGetUpdateInfoKeepsLastGoodAnswerWhenRefreshFails(t *testing.T) {
	latest := nextRevisionTag(t, 1)
	var failing atomic.Bool
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		serveLatest(latest)(w, r)
	})
	if _, err := (&PanelService{}).GetUpdateInfo(); err != nil {
		t.Fatal(err)
	}
	failing.Store(true)
	expireUpdateInfoCache(t)

	got, err := (&PanelService{}).GetUpdateInfo()
	if err != nil {
		t.Fatalf("refresh failure replaced the last good answer: %v", err)
	}
	if got.LatestVersion != latest || !got.UpdateAvailable {
		t.Fatalf("GetUpdateInfo = %+v, want the cached %s update", *got, latest)
	}
}

func TestGetUpdateInfoBacksOffAfterFailure(t *testing.T) {
	hits := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	for range 3 {
		if _, err := (&PanelService{}).GetUpdateInfo(); err == nil {
			t.Fatal("GetUpdateInfo succeeded against a failing API")
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("GitHub requests for 3 failing checks inside the retry window = %d, want 1", got)
	}
}

// expireUpdateInfoCache ages the cached answer past its TTL.
func expireUpdateInfoCache(t *testing.T) {
	t.Helper()
	updateInfoCache.mu.Lock()
	updateInfoCache.fetchedAt = time.Now().Add(-2 * updateInfoTTL)
	updateInfoCache.mu.Unlock()
}
