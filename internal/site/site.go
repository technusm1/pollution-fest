// Package site renders the static dashboard: embedded web assets plus
// compact per-day JSON partitions the browser filters client-side, so any
// start/end interval works without a server.
package site

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"pollution-fest/internal/firms"
	"pollution-fest/internal/geo"
	"pollution-fest/internal/report"
)

//go:embed web
var webFS embed.FS

// Manifest is data/manifest.json.
type Manifest struct {
	GeneratedAt   time.Time  `json:"generated_at"`
	LastDetection *time.Time `json:"last_detection,omitempty"`
	States        []string   `json:"states"`
	Sources       []string   `json:"sources"`
	Districts     []District `json:"districts"`
	Days          []Day      `json:"days"` // IST dates with data files
	Columns       []string   `json:"columns"`
}

type District struct {
	State string `json:"state"`
	Name  string `json:"name"`
}

type Day struct {
	Date  string `json:"date"`
	Count int    `json:"n"`
}

var columns = []string{"lat", "lon", "t_unix", "frp_mw", "conf", "src", "district"}

// Build writes the site into outDir.
func Build(outDir string, loc *geo.Locator, located []report.Located, now time.Time) error {
	if err := os.RemoveAll(filepath.Join(outDir, "data")); err != nil {
		return err
	}
	if err := copyWeb(outDir); err != nil {
		return err
	}
	dataDir := filepath.Join(outDir, "data", "days")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	m := Manifest{GeneratedAt: now.UTC(), States: loc.States(), Columns: columns}
	for _, d := range loc.Districts {
		m.Districts = append(m.Districts, District{State: d.State, Name: d.Name})
	}
	srcIdx := map[string]int{}
	for _, f := range firms.Families {
		srcIdx[f] = len(m.Sources)
		m.Sources = append(m.Sources, f)
	}

	byDay := map[string][][]any{}
	var last time.Time
	for _, l := range located {
		fam := firms.Family(l.Source)
		si, ok := srcIdx[fam]
		if !ok {
			si = len(m.Sources)
			srcIdx[fam] = si
			m.Sources = append(m.Sources, fam)
		}
		day := l.Time.In(report.IST).Format("2006-01-02")
		byDay[day] = append(byDay[day], []any{
			round(l.Lat, 4), round(l.Lon, 4), l.Time.Unix(), round(l.FRP, 1),
			int(l.Confidence), si, l.District.ID,
		})
		if l.Time.After(last) {
			last = l.Time
		}
	}
	if !last.IsZero() {
		m.LastDetection = &last
	}

	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		rows := byDay[d]
		sort.Slice(rows, func(i, j int) bool { return rows[i][2].(int64) < rows[j][2].(int64) })
		if err := writeJSON(filepath.Join(dataDir, d+".json"), rows); err != nil {
			return err
		}
		m.Days = append(m.Days, Day{Date: d, Count: len(rows)})
	}

	if err := writeJSON(filepath.Join(outDir, "data", "seasons.json"), buildSeasons(located, last, now)); err != nil {
		return err
	}

	gj, err := loc.SimplifiedGeoJSON(0.003)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "data", "districts.geojson"), gj, 0o644); err != nil {
		return err
	}
	return writeJSON(filepath.Join(outDir, "data", "manifest.json"), m)
}

func copyWeb(outDir string) error {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(outDir, p)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("site: marshal %s: %w", path, err)
	}
	return os.WriteFile(path, b, 0o644)
}

func round(f float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(f*p) / p
}
