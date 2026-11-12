package firms

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const viirsCSV = `latitude,longitude,bright_ti4,scan,track,acq_date,acq_time,satellite,instrument,confidence,version,bright_ti5,frp,daynight
30.90123,75.85432,330.5,0.39,0.36,2026-11-10,812,N,VIIRS,n,2.0NRT,295.1,12.4,D
29.5,76.1,340.1,0.4,0.37,2026-11-10,2045,N20,VIIRS,h,2.0NRT,290.2,33.0,N
`

const modisCSV = `latitude,longitude,brightness,scan,track,acq_date,acq_time,satellite,instrument,confidence,version,bright_t31,frp,daynight,type
30.1,75.2,320.0,1.0,1.0,2026-11-09,0530,Aqua,MODIS,85,6.1,300.0,40.5,D,0
30.2,75.3,320.0,1.0,1.0,2026-11-09,0530,Aqua,MODIS,20,6.1,300.0,5.0,D,2
`

func TestParseVIIRS(t *testing.T) {
	dets, err := ParseCSV(strings.NewReader(viirsCSV), "VIIRS_SNPP_NRT")
	if err != nil {
		t.Fatal(err)
	}
	if len(dets) != 2 {
		t.Fatalf("got %d detections", len(dets))
	}
	d := dets[0]
	want := time.Date(2026, 11, 10, 8, 12, 0, 0, time.UTC)
	if !d.Time.Equal(want) {
		t.Errorf("time = %v, want %v (acq_time without leading zero)", d.Time, want)
	}
	if d.Confidence != ConfNominal || dets[1].Confidence != ConfHigh {
		t.Errorf("confidence = %v, %v", d.Confidence, dets[1].Confidence)
	}
	if d.Type != -1 || d.FRP != 12.4 || d.Source != "VIIRS_SNPP_NRT" {
		t.Errorf("unexpected detection %+v", d)
	}
}

func TestParseMODIS(t *testing.T) {
	dets, err := ParseCSV(strings.NewReader(modisCSV), "MODIS_SP")
	if err != nil {
		t.Fatal(err)
	}
	if dets[0].Confidence != ConfHigh || dets[1].Confidence != ConfLow {
		t.Errorf("modis confidence mapping wrong: %v %v", dets[0].Confidence, dets[1].Confidence)
	}
	if dets[1].Type != 2 {
		t.Errorf("type = %d", dets[1].Type)
	}
}

func TestParseErrorBody(t *testing.T) {
	_, err := ParseCSV(strings.NewReader("Invalid MAP_KEY."), "VIIRS_SNPP_NRT")
	if !errors.Is(err, ErrNotCSV) {
		t.Fatalf("err = %v, want ErrNotCSV", err)
	}
	dets, err := ParseCSV(strings.NewReader(""), "VIIRS_SNPP_NRT")
	if err != nil || len(dets) != 0 {
		t.Fatalf("empty body: %v %v", dets, err)
	}
}

func TestParseAvailability(t *testing.T) {
	av, err := ParseAvailability(strings.NewReader("data_id,min_date,max_date\nVIIRS_SNPP_SP,2012-01-20,2026-06-30\nVIIRS_SNPP_NRT,2026-07-01,2026-11-12\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !av["VIIRS_SNPP_SP"].Covers(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Error("SP should cover its max date")
	}
}

func d(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestPlan(t *testing.T) {
	avail := map[string]DateRange{
		"VIIRS_SNPP_SP":  {Min: d("2012-01-20"), Max: d("2026-06-30")},
		"VIIRS_SNPP_NRT": {Min: d("2026-06-01"), Max: d("2026-11-12")},
	}
	got := Plan("VIIRS_SNPP", d("2026-06-28"), d("2026-07-08"), avail)
	want := []Chunk{
		{"VIIRS_SNPP_SP", d("2026-06-28"), 3},
		{"VIIRS_SNPP_NRT", d("2026-07-01"), 5},
		{"VIIRS_SNPP_NRT", d("2026-07-06"), 3},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Source != want[i].Source || !got[i].Start.Equal(want[i].Start) || got[i].Days != want[i].Days {
			t.Errorf("chunk %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(Plan("VIIRS_NOAA21", d("2026-01-01"), d("2026-01-05"), avail)) != 0 {
		t.Error("unavailable family should yield no chunks")
	}
}
