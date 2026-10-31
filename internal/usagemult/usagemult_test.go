package usagemult

import (
	"regexp"
	"testing"
)

var statEmailShape = regexp.MustCompile(`^nxm_[0-9]+_[a-z2-7]{16}$`)

func TestStatEmailIsDeterministicAndCaseFolded(t *testing.T) {
	a := StatEmail(7, "Alice@Example.com")
	if b := StatEmail(7, "alice@example.com"); a != b {
		t.Fatalf("case variants map to different stat emails: %q vs %q", a, b)
	}
	if again := StatEmail(7, "Alice@Example.com"); again != a {
		t.Fatalf("not deterministic: %q then %q", a, again)
	}
	if !statEmailShape.MatchString(a) {
		t.Fatalf("stat email %q does not match %s", a, statEmailShape)
	}
	// Pinned (computed with Python's hashlib/base64): a changed derivation orphans live counters.
	if pinned := StatEmail(1, "user"); pinned != "nxm_1_zx7omsbtyf75mblu" {
		t.Fatalf("derivation changed: StatEmail(1, \"user\") = %q", pinned)
	}
}

func TestStatEmailDiffersPerInboundAndEmail(t *testing.T) {
	seen := map[string]string{}
	for _, c := range []struct {
		id    int
		email string
	}{{1, "a"}, {2, "a"}, {1, "b"}, {12, "a"}, {21, "a"}} {
		s := StatEmail(c.id, c.email)
		if prev, dup := seen[s]; dup {
			t.Fatalf("collision: %q for both %s and %d/%s", s, prev, c.id, c.email)
		}
		seen[s] = c.email
	}
}

func TestParseStatEmail(t *testing.T) {
	cases := []struct {
		in     string
		wantID int
		wantOK bool
	}{
		{StatEmail(42, "bob"), 42, true},
		{"bob@example.com", 0, false},
		{"nxm_42_tooshort", 0, false},
		{"nxm_x_aaaaaaaaaaaaaaaa", 0, false},
		{"nxm_0_aaaaaaaaaaaaaaaa", 0, false},
		{"nxm_-3_aaaaaaaaaaaaaaaa", 0, false},
		{"nxm_42_AAAAAAAAAAAAAAAA", 0, false},
		{"nxm_42_aaaaaaaaaaaaaaa1", 0, false},
		{"hmstat_42_aaaaaaaaaaaaaaaa", 0, false},
	}
	for _, c := range cases {
		id, ok := ParseStatEmail(c.in)
		if ok != c.wantOK || id != c.wantID {
			t.Errorf("ParseStatEmail(%q) = (%d, %v), want (%d, %v)", c.in, id, ok, c.wantID, c.wantOK)
		}
	}
}

func TestMatchesStatEmail(t *testing.T) {
	s := StatEmail(5, "Carol")
	if !Matches(s, 5, "carol") {
		t.Fatal("stat email does not verify against its own inbound/email")
	}
	if Matches(s, 6, "carol") || Matches(s, 5, "dave") {
		t.Fatal("stat email verified against the wrong inbound or email")
	}
}

func TestSupportsProtocol(t *testing.T) {
	for _, p := range []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria", "masque"} {
		if !SupportsProtocol(p) {
			t.Errorf("%s must be supported", p)
		}
	}
	for _, p := range []string{"wireguard", "tuic", "mtproto", "amneziawg", "tunnel", "http", "mixed", "tun", ""} {
		if SupportsProtocol(p) {
			t.Errorf("%s must not be supported in step 1", p)
		}
	}
}

func TestActiveLatch(t *testing.T) {
	t.Cleanup(func() { SetActive(false) })
	if Active() {
		t.Fatal("latch must start OFF (stock behaviour)")
	}
	SetActive(true)
	if !Active() {
		t.Fatal("SetActive(true) not observed")
	}
}
