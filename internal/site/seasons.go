package site

import (
	"sort"
	"time"

	"paraliwatch/internal/firms"
	"paraliwatch/internal/report"
)

// SeasonFamily is the satellite used for year-over-year comparison. VIIRS on
// NOAA-20 has continuous standard-processing coverage since 2018, whereas
// S-NPP stopped on 2026-11-02 and NOAA-21 only starts in 2023, so mixing
// satellites would fake a trend.
const SeasonFamily = "VIIRS_NOAA20"

// Kharif paddy-residue season window (IST), month/day.
const (
	seasonStartMonth, seasonStartDay = time.September, 15
	seasonEndMonth, seasonEndDay     = time.November, 30
)

// Season is one year's kharif-season tally from SeasonFamily only.
type Season struct {
	Year int `json:"year"`
	// ToDate counts 15 Sep → the same month/day/time as the latest detection,
	// so a partial current season is compared like for like.
	ToDate     map[string]int `json:"to_date"`
	ToDateAll  int            `json:"to_date_total"`
	Full       map[string]int `json:"full"`
	FullAll    int            `json:"full_total"`
	Complete   bool           `json:"complete"`
	After3PMTD int            `json:"after_3pm_to_date"`
}

// Seasons is data/seasons.json.
type Seasons struct {
	Satellite string    `json:"satellite"`
	Window    string    `json:"window"`
	CutoffMD  string    `json:"cutoff"` // MM-DD HH:MM IST used for to_date
	Years     []Season  `json:"years"`
	Generated time.Time `json:"generated_at"`
}

func buildSeasons(located []report.Located, last, now time.Time) Seasons {
	out := Seasons{Satellite: SeasonFamily, Window: "15 Sep – 30 Nov (IST)", Generated: now.UTC()}
	if last.IsZero() {
		return out
	}
	lastIST := last.In(report.IST)
	out.CutoffMD = lastIST.Format("01-02 15:04")

	byYear := map[int]*Season{}
	for _, l := range located {
		if l.Confidence == firms.ConfLow || firms.Family(l.Source) != SeasonFamily {
			continue
		}
		t := l.Time.In(report.IST)
		y := t.Year()
		start := time.Date(y, seasonStartMonth, seasonStartDay, 0, 0, 0, 0, report.IST)
		end := time.Date(y, seasonEndMonth, seasonEndDay+1, 0, 0, 0, 0, report.IST)
		if t.Before(start) || !t.Before(end) {
			continue
		}
		s, ok := byYear[y]
		if !ok {
			s = &Season{Year: y, ToDate: map[string]int{}, Full: map[string]int{}}
			byYear[y] = s
		}
		st := l.District.State
		s.Full[st]++
		s.FullAll++
		cutoff := time.Date(y, lastIST.Month(), lastIST.Day(), lastIST.Hour(), lastIST.Minute(), 0, 0, report.IST)
		if !t.After(cutoff) {
			s.ToDate[st]++
			s.ToDateAll++
			if t.Hour() >= 15 {
				s.After3PMTD++
			}
		}
	}
	for _, s := range byYear {
		s.Complete = !lastIST.Before(time.Date(s.Year, seasonEndMonth, seasonEndDay+1, 0, 0, 0, 0, report.IST))
		out.Years = append(out.Years, *s)
	}
	sort.Slice(out.Years, func(i, j int) bool { return out.Years[i].Year < out.Years[j].Year })
	return out
}
