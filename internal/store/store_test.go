package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pollution-fest/internal/firms"
)

func TestWriteLoadAndSPPrecedence(t *testing.T) {
	st := Store{Root: t.TempDir()}
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	det := firms.Detection{Lat: 30.9, Lon: 75.85, Time: day.Add(8 * time.Hour), Source: "VIIRS_SNPP_NRT", Confidence: firms.ConfHigh, FRP: 10, DayNight: "D", Type: -1}

	if _, err := st.Write(day, "VIIRS_SNPP", "NRT", []firms.Detection{det, det}); err != nil {
		t.Fatal(err)
	}
	// Re-writing replaces rather than appends.
	if _, err := st.Write(day, "VIIRS_SNPP", "NRT", []firms.Detection{det}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(day, day)
	if err != nil || len(got) != 1 {
		t.Fatalf("load after rewrite: %d, %v", len(got), err)
	}
	if got[0].Confidence != firms.ConfHigh || !got[0].Time.Equal(det.Time) {
		t.Errorf("round trip mismatch: %+v", got[0])
	}

	sp := det
	sp.Source = "VIIRS_SNPP_SP"
	sp.Type = 0
	if _, err := st.Write(day, "VIIRS_SNPP", "SP", []firms.Detection{sp}); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.Product(day, "VIIRS_SNPP"); p != "SP" {
		t.Fatalf("product = %s, want SP", p)
	}
	res, err := st.Write(day, "VIIRS_SNPP", "NRT", nil)
	if err != nil || res != SkippedHaveSP {
		t.Fatalf("NRT over SP: res=%v err=%v", res, err)
	}
	got, _ = st.Load(day, day)
	if len(got) != 1 || got[0].Source != "VIIRS_SNPP_SP" {
		t.Fatalf("expected only SP row, got %+v", got)
	}

	// Empty partition is recorded.
	empty := day.AddDate(0, 0, 1)
	if _, err := st.Write(empty, "MODIS", "NRT", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Product(empty, "MODIS"); !ok {
		t.Error("empty partition should exist")
	}
}

func TestCompactAndLegacyCSV(t *testing.T) {
	root := t.TempDir()
	st := Store{Root: root}
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	in := firms.Detection{Lat: 30.9, Lon: 75.85, Time: day.Add(8 * time.Hour), Source: "MODIS_NRT", Type: -1}

	// Simulate a legacy plain-CSV partition.
	dir := filepath.Join(root, "2026-11-10")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "latitude,longitude,acq_utc,source,satellite,instrument,confidence,frp,daynight,type\n" +
		"30.90000,75.85000,2026-11-10T08:00:00Z,MODIS_NRT,Aqua,MODIS,high,5.00,D,-1\n" +
		"24.86000,67.00000,2026-11-10T08:00:00Z,MODIS_NRT,Aqua,MODIS,high,5.00,D,-1\n"
	if err := os.WriteFile(filepath.Join(dir, "MODIS.NRT.csv"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Load(day, day); len(got) != 2 {
		t.Fatalf("legacy load = %d rows", len(got))
	}
	before, after, err := st.Compact(func(d firms.Detection) bool { return d.Lat == in.Lat })
	if err != nil || before != 2 || after != 1 {
		t.Fatalf("compact: before=%d after=%d err=%v", before, after, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "MODIS.NRT.csv")); !os.IsNotExist(err) {
		t.Error("legacy csv should be removed")
	}
	got, err := st.Load(day, day)
	if err != nil || len(got) != 1 || got[0].Lat != in.Lat {
		t.Fatalf("after compact: %+v %v", got, err)
	}
}
