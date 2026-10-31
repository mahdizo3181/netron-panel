package model

import (
	"math"
	"testing"
)

func TestEffectiveUsageMultiplier(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name        string
		stored      *float64
		want        float64
		wantClamped bool
	}{
		{"NULL from a pre-feature row or old peer is x1", nil, 1, false},
		{"zero is free, not unset", f(0), 0, false},
		{"below one is kept", f(0.5), 0.5, false},
		{"upper bound kept", f(10), 10, false},
		{"above max clamps to max", f(25), 10, true},
		{"negative never becomes free", f(-1), 1, true},
		{"NaN never becomes free", f(math.NaN()), 1, true},
		{"+Inf clamps to max", f(math.Inf(1)), 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, clamped := (&Inbound{UsageMultiplier: tc.stored}).EffectiveUsageMultiplier()
			if got != tc.want || clamped != tc.wantClamped {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, clamped, tc.want, tc.wantClamped)
			}
		})
	}
}
