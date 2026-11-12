package report

import (
	"testing"
	"time"

	"pollution-fest/internal/firms"
	"pollution-fest/internal/geo"
)

func TestLocateAndSummarize(t *testing.T) {
	loc, err := geo.NewLocator(geo.DefaultStates)
	if err != nil {
		t.Fatal(err)
	}
	at := func(hhmmIST string) time.Time {
		tm, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-10 "+hhmmIST, IST)
		return tm.UTC()
	}
	dets := []firms.Detection{
		{Lat: 30.901, Lon: 75.857, Time: at("13:40"), Source: "VIIRS_SNPP_NRT", Confidence: firms.ConfNominal, FRP: 10, DayNight: "D", Type: -1},
		{Lat: 30.901, Lon: 75.857, Time: at("16:10"), Source: "VIIRS_NOAA20_NRT", Confidence: firms.ConfHigh, FRP: 20, DayNight: "D", Type: -1},
		{Lat: 29.686, Lon: 76.990, Time: at("14:00"), Source: "MODIS_SP", Confidence: firms.ConfLow, FRP: 5, DayNight: "D", Type: 0},
		{Lat: 29.686, Lon: 76.990, Time: at("14:00"), Source: "MODIS_SP", Confidence: firms.ConfHigh, FRP: 99, DayNight: "D", Type: 2}, // industrial
		{Lat: 19.07, Lon: 72.87, Time: at("14:00"), Source: "MODIS_SP", Confidence: firms.ConfHigh, FRP: 1, DayNight: "D", Type: 0},    // Mumbai
	}
	located := Locate(dets, loc)
	if len(located) != 3 {
		t.Fatalf("located %d, want 3 (industrial and out-of-area dropped)", len(located))
	}

	from, _ := time.ParseInLocation("2006-01-02", "2026-11-10", IST)
	s := Summarize(located, Filter{From: from, To: from.AddDate(0, 0, 1), MinConf: firms.ConfNominal})
	if s.Total != 2 {
		t.Fatalf("total = %d, want 2 (low-confidence excluded)", s.Total)
	}
	if s.AfterThreePM != 1 || s.ByHourIST[13] != 1 || s.ByHourIST[16] != 1 {
		t.Errorf("hour buckets wrong: after3=%d hours=%v", s.AfterThreePM, s.ByHourIST)
	}
	if len(s.ByState) != 1 || s.ByState[0].Key != "Punjab" || s.ByDistrict[0].Key != "Ludhiana" {
		t.Errorf("aggregation wrong: %+v %+v", s.ByState, s.ByDistrict)
	}
	if len(s.ByDayIST) != 1 || s.ByDayIST[0].Key != "2026-11-10" {
		t.Errorf("by day wrong: %+v", s.ByDayIST)
	}
}
