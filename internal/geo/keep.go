package geo

import (
	"embed"
	"fmt"
	"strings"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/planar"
)

//go:embed boundaries/extra/*.geojson
var extraFS embed.FS

// ExtraRegions are areas outside the dashboard's states whose raw detections we
// still keep, keyed by name -> embedded file. Pakistani Punjab burns paddy
// stubble in the same weeks and its smoke also reaches Delhi.
var ExtraRegions = map[string]string{
	"pakistan-punjab": "boundaries/extra/pakistan-punjab.geojson",
}

// DefaultExtras are kept unless the caller says otherwise.
var DefaultExtras = []string{"pakistan-punjab"}

// keepTolerance (degrees, ~2 km) absorbs slivers where boundary datasets from
// different sources don't line up exactly along shared borders.
const keepTolerance = 0.02

type boundedGeom struct {
	geom  orb.Geometry
	bound orb.Bound
}

// Keeper decides which raw detections are worth storing: anything inside the
// covered districts or an extra region, with a small tolerance.
type Keeper struct {
	geoms []boundedGeom
	bound orb.Bound
}

// NewKeeper combines the locator's districts with the named extra regions.
func NewKeeper(loc *Locator, extras []string) (*Keeper, error) {
	k := &Keeper{bound: loc.Bound()}
	for _, d := range loc.Districts {
		k.geoms = append(k.geoms, boundedGeom{d.Geom, d.Bound.Pad(keepTolerance)})
	}
	for _, name := range extras {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		file, ok := ExtraRegions[name]
		if !ok {
			return nil, fmt.Errorf("geo: unknown extra region %q", name)
		}
		raw, err := extraFS.ReadFile(file)
		if err != nil {
			return nil, err
		}
		fc, err := geojson.UnmarshalFeatureCollection(raw)
		if err != nil {
			return nil, fmt.Errorf("geo: %s: %w", file, err)
		}
		for _, f := range fc.Features {
			b := f.Geometry.Bound()
			k.geoms = append(k.geoms, boundedGeom{f.Geometry, b.Pad(keepTolerance)})
			k.bound = k.bound.Union(b)
		}
	}
	k.bound = k.bound.Pad(keepTolerance)
	return k, nil
}

// Bound is the union bounding box of everything kept (padded).
func (k *Keeper) Bound() orb.Bound { return k.bound }

// Keep reports whether (lat, lon) falls inside, or within ~2 km of, a kept area.
func (k *Keeper) Keep(lat, lon float64) bool {
	p := orb.Point{lon, lat}
	if !k.bound.Contains(p) {
		return false
	}
	probes := [5]orb.Point{p,
		{lon + keepTolerance, lat}, {lon - keepTolerance, lat},
		{lon, lat + keepTolerance}, {lon, lat - keepTolerance}}
	for _, g := range k.geoms {
		if !g.bound.Contains(p) {
			continue
		}
		for _, q := range probes {
			if contains(g.geom, q) {
				return true
			}
		}
	}
	return false
}

func contains(g orb.Geometry, p orb.Point) bool {
	switch v := g.(type) {
	case orb.Polygon:
		return planar.PolygonContains(v, p)
	case orb.MultiPolygon:
		return planar.MultiPolygonContains(v, p)
	}
	return false
}
