package panel

import "testing"

func TestIsNewerVersionNetronTags(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v3.11.0-netron.1", "3.11.0-netron.1", false},
		{"v3.11.0-netron.2", "3.11.0-netron.1", true},
		{"v3.11.0-netron.1", "3.11.0-netron.2", false},
		{"v3.12.0-netron.1", "3.11.0-netron.3", true},
	}
	for _, tc := range cases {
		if got := isNewerVersion(tc.latest, tc.current); got != tc.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}
