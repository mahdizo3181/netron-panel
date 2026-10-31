package version

import "testing"

// Fork tags (v3.11.0-netron.1) must order by upstream base, then fork revision;
// an unparsed suffix made the notifier offer the installed release forever.
func TestCompareNetronRevision(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v3.11.0-netron.1", "3.11.0-netron.1", 0},
		{"v3.11.0-netron.2", "3.11.0-netron.1", 1},
		{"v3.11.0-netron.10", "3.11.0-netron.9", 1},
		{"v3.11.0-netron.1", "3.11.0", 1},
		{"v3.12.0-netron.1", "3.11.0-netron.9", 1},
		{"v3.11.0", "3.11.0-netron.1", -1},
		{"v3.10.9-netron.5", "3.11.0", -1},
	}
	for _, tc := range cases {
		got, ok := Compare(tc.a, tc.b)
		if !ok || got != tc.want {
			t.Errorf("Compare(%q, %q) = (%d, %v), want (%d, true)", tc.a, tc.b, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"3.11.0-netron", "3.11.0-netron.x", "3.11.0-netron.1-rc.1", "3.11.0-rc1", "3.11.0-netron.-1"} {
		if _, ok := Compare(bad, "3.11.0"); ok {
			t.Errorf("Compare(%q, ...) accepted an unexpected suffix", bad)
		}
	}
}
