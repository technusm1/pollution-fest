package geo

import "testing"

func TestLocate(t *testing.T) {
	loc, err := NewLocator(DefaultStates)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name          string
		lat, lon      float64
		state, distr  string
		wantContained bool
	}{
		{"Ludhiana city", 30.901, 75.857, "Punjab", "Ludhiana", true},
		{"Karnal", 29.686, 76.990, "Haryana", "Karnal", true},
		{"Connaught Place", 28.6315, 77.2167, "Delhi", "", true},
		{"Lucknow", 26.8467, 80.9462, "Uttar Pradesh", "Lucknow", true},
		{"Mumbai (not covered)", 19.076, 72.8777, "", "", false},
	}
	for _, c := range cases {
		got, ok := loc.Locate(c.lat, c.lon)
		if ok != c.wantContained {
			t.Errorf("%s: contained=%v, want %v", c.name, ok, c.wantContained)
			continue
		}
		if !ok {
			continue
		}
		if got.State != c.state || (c.distr != "" && got.Name != c.distr) {
			t.Errorf("%s: got %s/%s, want %s/%s", c.name, got.State, got.Name, c.state, c.distr)
		}
	}
}

func TestUnknownState(t *testing.T) {
	if _, err := NewLocator([]string{"Atlantis"}); err == nil {
		t.Error("expected error for unknown state")
	}
}

func TestKeeper(t *testing.T) {
	loc, err := NewLocator(DefaultStates)
	if err != nil {
		t.Fatal(err)
	}
	k, err := NewKeeper(loc, DefaultExtras)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		lat, lon float64
		want     bool
	}{
		{"Ludhiana", 30.901, 75.857, true},
		{"Lahore (Pakistani Punjab)", 31.5204, 74.3587, true},
		{"Gujranwala (Pakistani Punjab)", 32.1877, 74.1945, true},
		{"Rawalpindi (north Pakistani Punjab)", 33.5651, 73.0169, true},
		{"Shimla (Himachal, not kept)", 31.1048, 77.1734, false},
		{"Gwalior (MP, not kept)", 26.2183, 78.1828, false},
		{"Karachi (Sindh, not kept)", 24.8607, 67.0011, false},
	}
	for _, c := range cases {
		if got := k.Keep(c.lat, c.lon); got != c.want {
			t.Errorf("%s: Keep=%v, want %v", c.name, got, c.want)
		}
	}
	if b := k.Bound(); b.Max[1] < 34 {
		t.Errorf("keeper bound should reach northern Pakistani Punjab, got %v", b)
	}
}
