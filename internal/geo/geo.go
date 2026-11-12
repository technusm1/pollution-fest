// Package geo maps fire detections to Indian states and districts using
// embedded district boundaries (Census 2011 district lines, via
// github.com/udit-001/india-maps-data).
package geo

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/planar"
	"github.com/paulmach/orb/simplify"
)

//go:embed boundaries/*.geojson
var boundaryFS embed.FS

// DefaultStates is the initial coverage: the north-Indian paddy belt plus the
// downwind NCR and hill states.
var DefaultStates = []string{"Punjab", "Haryana", "Uttar Pradesh", "Rajasthan", "Delhi", "Uttarakhand"}

// District is one administrative district polygon.
type District struct {
	ID    int
	State string
	Name  string
	Geom  orb.Geometry
	Bound orb.Bound
}

// Locator answers "which district is this point in?".
type Locator struct {
	Districts []District
	bound     orb.Bound
}

// NewLocator loads the embedded boundaries for the given states (case-insensitive).
func NewLocator(states []string) (*Locator, error) {
	want := make(map[string]bool, len(states))
	for _, s := range states {
		want[strings.ToLower(strings.TrimSpace(s))] = true
	}
	files, err := fs.Glob(boundaryFS, "boundaries/*.geojson")
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	loc := &Locator{}
	found := map[string]bool{}
	for _, f := range files {
		raw, err := boundaryFS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		fc, err := geojson.UnmarshalFeatureCollection(raw)
		if err != nil {
			return nil, fmt.Errorf("geo: %s: %w", path.Base(f), err)
		}
		for _, feat := range fc.Features {
			state := feat.Properties.MustString("st_nm", "")
			if !want[strings.ToLower(state)] {
				continue
			}
			found[strings.ToLower(state)] = true
			switch feat.Geometry.(type) {
			case orb.Polygon, orb.MultiPolygon:
			default:
				continue
			}
			d := District{
				ID:    len(loc.Districts),
				State: state,
				Name:  feat.Properties.MustString("district", state),
				Geom:  feat.Geometry,
				Bound: feat.Geometry.Bound(),
			}
			if len(loc.Districts) == 0 {
				loc.bound = d.Bound
			} else {
				loc.bound = loc.bound.Union(d.Bound)
			}
			loc.Districts = append(loc.Districts, d)
		}
	}
	for s := range want {
		if !found[s] {
			return nil, fmt.Errorf("geo: no boundaries for state %q", s)
		}
	}
	return loc, nil
}

// Bound returns the union bounding box of all loaded districts.
func (l *Locator) Bound() orb.Bound { return l.bound }

// Locate returns the district containing (lat, lon), or ok=false.
func (l *Locator) Locate(lat, lon float64) (District, bool) {
	p := orb.Point{lon, lat}
	if !l.bound.Contains(p) {
		return District{}, false
	}
	for _, d := range l.Districts {
		if !d.Bound.Contains(p) {
			continue
		}
		switch g := d.Geom.(type) {
		case orb.Polygon:
			if planar.PolygonContains(g, p) {
				return d, true
			}
		case orb.MultiPolygon:
			if planar.MultiPolygonContains(g, p) {
				return d, true
			}
		}
	}
	return District{}, false
}

// States returns the distinct state names in load order.
func (l *Locator) States() []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range l.Districts {
		if !seen[d.State] {
			seen[d.State] = true
			out = append(out, d.State)
		}
	}
	return out
}

// SimplifiedGeoJSON renders districts as a GeoJSON FeatureCollection with
// simplified geometry (tolerance in degrees) for the web map.
func (l *Locator) SimplifiedGeoJSON(tolerance float64) ([]byte, error) {
	fc := geojson.NewFeatureCollection()
	s := simplify.DouglasPeucker(tolerance)
	for _, d := range l.Districts {
		g := s.Simplify(orb.Clone(d.Geom))
		f := geojson.NewFeature(roundGeom(g))
		f.Properties["id"] = d.ID
		f.Properties["state"] = d.State
		f.Properties["district"] = d.Name
		fc.Append(f)
	}
	return json.Marshal(fc)
}

func roundGeom(g orb.Geometry) orb.Geometry {
	r := func(p orb.Point) orb.Point {
		return orb.Point{round4(p[0]), round4(p[1])}
	}
	switch v := g.(type) {
	case orb.Polygon:
		for i := range v {
			for j := range v[i] {
				v[i][j] = r(v[i][j])
			}
		}
		return v
	case orb.MultiPolygon:
		for i := range v {
			for j := range v[i] {
				for k := range v[i][j] {
					v[i][j][k] = r(v[i][j][k])
				}
			}
		}
		return v
	}
	return g
}

func round4(f float64) float64 {
	if f < 0 {
		return float64(int64(f*1e4-0.5)) / 1e4
	}
	return float64(int64(f*1e4+0.5)) / 1e4
}
