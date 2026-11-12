// Package report filters located detections to an interval and aggregates them.
package report

import (
	"sort"
	"time"

	"pollution-fest/internal/firms"
	"pollution-fest/internal/geo"
)

// IST is India Standard Time (UTC+05:30, no DST).
var IST = time.FixedZone("IST", 5*3600+30*60)

// Located is a detection that fell inside a covered district.
type Located struct {
	firms.Detection
	District geo.District
}

// Locate drops detections outside the covered districts and FIRMS static
// land sources (type 2: refineries, kilns, etc. — only flagged in SP data).
func Locate(dets []firms.Detection, loc *geo.Locator) []Located {
	out := make([]Located, 0, len(dets))
	for _, d := range dets {
		if d.Type == 2 || d.Type == 3 {
			continue
		}
		if dist, ok := loc.Locate(d.Lat, d.Lon); ok {
			out = append(out, Located{Detection: d, District: dist})
		}
	}
	return out
}

// Filter selects detections. From is inclusive, To exclusive.
type Filter struct {
	From, To time.Time
	MinConf  firms.Confidence
	States   map[string]bool // nil = all
}

func (f Filter) Match(l Located) bool {
	if l.Time.Before(f.From) || !l.Time.Before(f.To) {
		return false
	}
	if l.Confidence != firms.ConfUnknown && l.Confidence < f.MinConf {
		return false
	}
	if f.States != nil && !f.States[l.District.State] {
		return false
	}
	return true
}

type Count struct {
	Key   string  `json:"key"`
	State string  `json:"state,omitempty"`
	N     int     `json:"n"`
	FRP   float64 `json:"frp_mw"`
}

type Summary struct {
	From          time.Time      `json:"from"`
	To            time.Time      `json:"to"`
	Total         int            `json:"total"`
	FRP           float64        `json:"frp_mw"`
	ByState       []Count        `json:"by_state"`
	ByDistrict    []Count        `json:"by_district"`
	ByDayIST      []Count        `json:"by_day_ist"`
	ByHourIST     [24]int        `json:"by_hour_ist"`
	BySource      map[string]int `json:"by_source"`
	AfterThreePM  int            `json:"after_3pm_ist"`
	DaytimePasses int            `json:"day_detections"`
}

// Summarize aggregates detections matching f.
func Summarize(all []Located, f Filter) Summary {
	s := Summary{From: f.From, To: f.To, BySource: map[string]int{}}
	byState := map[string]*Count{}
	byDist := map[string]*Count{}
	byDay := map[string]*Count{}
	bump := func(m map[string]*Count, key, state string, frp float64) {
		mk := state + "|" + key // district names repeat across states (e.g. Pratapgarh)
		c, ok := m[mk]
		if !ok {
			c = &Count{Key: key, State: state}
			m[mk] = c
		}
		c.N++
		c.FRP += frp
	}
	for _, l := range all {
		if !f.Match(l) {
			continue
		}
		s.Total++
		s.FRP += l.FRP
		bump(byState, l.District.State, "", l.FRP)
		bump(byDist, l.District.Name, l.District.State, l.FRP)
		local := l.Time.In(IST)
		bump(byDay, local.Format("2006-01-02"), "", l.FRP)
		s.ByHourIST[local.Hour()]++
		if local.Hour() >= 15 {
			s.AfterThreePM++
		}
		if l.DayNight == "D" {
			s.DaytimePasses++
		}
		s.BySource[firms.Family(l.Source)]++
	}
	s.ByState = sorted(byState, byCountDesc)
	s.ByDistrict = sorted(byDist, byCountDesc)
	s.ByDayIST = sorted(byDay, func(a, b Count) bool { return a.Key < b.Key })
	return s
}

func byCountDesc(a, b Count) bool {
	if a.N != b.N {
		return a.N > b.N
	}
	return a.Key < b.Key
}

func sorted(m map[string]*Count, less func(a, b Count) bool) []Count {
	out := make([]Count, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}
