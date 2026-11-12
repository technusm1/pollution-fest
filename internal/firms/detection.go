// Package firms talks to NASA FIRMS (Fire Information for Resource Management
// System) and normalises its active-fire detections.
package firms

import (
	"fmt"
	"strings"
	"time"
)

// Confidence is a normalised detection confidence across VIIRS (l/n/h) and
// MODIS (0-100) products.
type Confidence uint8

const (
	ConfUnknown Confidence = iota
	ConfLow
	ConfNominal
	ConfHigh
)

func (c Confidence) String() string {
	switch c {
	case ConfLow:
		return "low"
	case ConfNominal:
		return "nominal"
	case ConfHigh:
		return "high"
	default:
		return "unknown"
	}
}

// ParseConfidence accepts VIIRS letters/words and MODIS percentages.
func ParseConfidence(s string) Confidence {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "l", "low":
		return ConfLow
	case "n", "nominal":
		return ConfNominal
	case "h", "high":
		return ConfHigh
	case "", "unknown":
		return ConfUnknown
	}
	var pct int
	if _, err := fmt.Sscanf(s, "%d", &pct); err != nil {
		return ConfUnknown
	}
	// MODIS collection 6 guidance: <30 low, 30-79 nominal, >=80 high.
	switch {
	case pct < 30:
		return ConfLow
	case pct < 80:
		return ConfNominal
	default:
		return ConfHigh
	}
}

// Detection is one thermal-anomaly pixel seen by one satellite pass.
// It is NOT a unique fire: the same field can appear on several satellites.
type Detection struct {
	Lat        float64
	Lon        float64
	Time       time.Time // acquisition time, UTC
	Source     string    // FIRMS product, e.g. VIIRS_SNPP_NRT
	Satellite  string
	Instrument string
	Confidence Confidence
	FRP        float64 // fire radiative power, MW
	DayNight   string  // "D" or "N"
	Type       int     // FIRMS type (SP only): 0 vegetation, 2 static land source, 3 offshore; -1 unknown
}

// Family strips the processing suffix: VIIRS_SNPP_NRT -> VIIRS_SNPP.
func Family(source string) string {
	for _, suf := range []string{"_NRT", "_SP"} {
		if strings.HasSuffix(source, suf) {
			return strings.TrimSuffix(source, suf)
		}
	}
	return source
}

// Product returns "NRT" or "SP" for a FIRMS source name.
func Product(source string) string {
	if strings.HasSuffix(source, "_SP") {
		return "SP"
	}
	return "NRT"
}

// Families are the satellite/instrument streams paraliwatch ingests.
// LANDSAT is US/Canada only, so it is excluded.
var Families = []string{"VIIRS_SNPP", "VIIRS_NOAA20", "VIIRS_NOAA21", "MODIS"}

// BBox is west,south,east,north in degrees.
type BBox struct {
	West, South, East, North float64
}

func (b BBox) String() string {
	return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", b.West, b.South, b.East, b.North)
}

func (b BBox) Contains(lat, lon float64) bool {
	return lon >= b.West && lon <= b.East && lat >= b.South && lat <= b.North
}
