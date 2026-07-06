package job

import "testing"

func TestMetaContextHigh(t *testing.T) {
	for _, tc := range []struct {
		name      string
		context   int
		threshold int
		want      bool
	}{
		{"below", 149000, 150000, false},
		{"equal", 150000, 150000, true},
		{"above", 180000, 150000, true},
		{"disabled", 999999, 0, false},
	} {
		m := &Meta{Context: tc.context}
		if got := m.ContextHigh(tc.threshold); got != tc.want {
			t.Errorf("%s: ContextHigh(%d) with ctx %d = %v, want %v",
				tc.name, tc.threshold, tc.context, got, tc.want)
		}
	}
}
