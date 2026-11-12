// Package store persists raw FIRMS detections as plain CSV partitions:
//
//	<root>/YYYY-MM-DD/<FAMILY>.<PRODUCT>.csv.gz
//
// (Plain .csv partitions from older versions are still read, and replaced on
// the next write or by Compact.)
// One file per UTC date per satellite family. A partition is replaced wholesale
// on every write, which makes re-fetching idempotent. SP (standard processing)
// partitions are never downgraded to NRT.
package store

import (
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pollution-fest/internal/firms"
)

var header = []string{"latitude", "longitude", "acq_utc", "source", "satellite", "instrument", "confidence", "frp", "daynight", "type"}

type Store struct {
	Root string
}

func dayDir(day time.Time) string { return day.UTC().Format("2006-01-02") }

// Product reports which product is stored for (day, family), if any.
func (s Store) Product(day time.Time, family string) (string, bool) {
	for _, p := range []string{"SP", "NRT"} {
		for _, fn := range []string{s.path(day, family, p), s.legacyPath(day, family, p)} {
			if _, err := os.Stat(fn); err == nil {
				return p, true
			}
		}
	}
	return "", false
}

func (s Store) path(day time.Time, family, product string) string {
	return filepath.Join(s.Root, dayDir(day), family+"."+product+".csv.gz")
}

func (s Store) legacyPath(day time.Time, family, product string) string {
	return filepath.Join(s.Root, dayDir(day), family+"."+product+".csv")
}

func removeIfExists(paths ...string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// WriteResult says what Write did.
type WriteResult int

const (
	Written WriteResult = iota
	SkippedHaveSP
)

// Write replaces the (day, family) partition with dets. An empty dets still
// writes a header-only file so "fetched, zero fires" is distinguishable from
// "never fetched".
func (s Store) Write(day time.Time, family, product string, dets []firms.Detection) (WriteResult, error) {
	if product == "NRT" {
		if p, ok := s.Product(day, family); ok && p == "SP" {
			return SkippedHaveSP, nil
		}
	}
	dir := filepath.Join(s.Root, dayDir(day))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	target := s.path(day, family, product)
	tmp, err := os.CreateTemp(dir, ".tmp-*.csv.gz")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())

	sorted := append([]firms.Detection(nil), dets...)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Time.Equal(sorted[j].Time) {
			return sorted[i].Time.Before(sorted[j].Time)
		}
		if sorted[i].Lat != sorted[j].Lat {
			return sorted[i].Lat < sorted[j].Lat
		}
		return sorted[i].Lon < sorted[j].Lon
	})

	gz, err := gzip.NewWriterLevel(tmp, gzip.BestCompression)
	if err != nil {
		tmp.Close()
		return 0, err
	}
	gz.ModTime = time.Time{} // deterministic output: identical data -> identical bytes -> no git churn
	w := csv.NewWriter(gz)
	_ = w.Write(header)
	for _, d := range sorted {
		_ = w.Write([]string{
			strconv.FormatFloat(d.Lat, 'f', 5, 64),
			strconv.FormatFloat(d.Lon, 'f', 5, 64),
			d.Time.UTC().Format(time.RFC3339),
			d.Source,
			d.Satellite,
			d.Instrument,
			d.Confidence.String(),
			strconv.FormatFloat(d.FRP, 'f', 2, 64),
			d.DayNight,
			strconv.Itoa(d.Type),
		})
	}
	w.Flush()
	if err := errors.Join(w.Error(), gz.Close()); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return 0, err
	}
	if err := removeIfExists(s.legacyPath(day, family, product)); err != nil {
		return 0, err
	}
	if product == "SP" {
		if err := removeIfExists(s.path(day, family, "NRT"), s.legacyPath(day, family, "NRT")); err != nil {
			return 0, err
		}
	}
	return Written, nil
}

// Load reads every partition with UTC date in [from, to] (inclusive dates).
func (s Store) Load(from, to time.Time) ([]firms.Detection, error) {
	days, err := s.Days()
	if err != nil {
		return nil, err
	}
	f, t := dayDir(from), dayDir(to)
	var out []firms.Detection
	for _, d := range days {
		if d < f || d > t {
			continue
		}
		files, err := s.partitionFiles(d)
		if err != nil {
			return nil, err
		}
		for _, fn := range files {
			dets, err := readFile(fn)
			if err != nil {
				return nil, err
			}
			out = append(out, dets...)
		}
	}
	return out, nil
}

// Days lists stored UTC dates (YYYY-MM-DD), sorted.
func (s Store) Days() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := time.Parse("2006-01-02", e.Name()); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s Store) partitionFiles(day string) ([]string, error) {
	var files []string
	for _, pat := range []string{"*.csv", "*.csv.gz"} {
		m, err := filepath.Glob(filepath.Join(s.Root, day, pat))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	sort.Strings(files)
	return files, nil
}

// Compact rewrites every partition as gzip, keeping only detections for which
// keep returns true. It returns rows before and after.
func (s Store) Compact(keep func(firms.Detection) bool) (before, after int, err error) {
	days, err := s.Days()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range days {
		day, _ := time.Parse("2006-01-02", d)
		files, err := s.partitionFiles(d)
		if err != nil {
			return before, after, err
		}
		for _, fn := range files {
			base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(fn), ".gz"), ".csv")
			family, product, ok := strings.Cut(base, ".")
			if !ok || (product != "SP" && product != "NRT") {
				return before, after, fmt.Errorf("store: unexpected partition name %s", fn)
			}
			dets, err := readFile(fn)
			if err != nil {
				return before, after, err
			}
			before += len(dets)
			kept := dets[:0]
			for _, det := range dets {
				if keep(det) {
					kept = append(kept, det)
				}
			}
			after += len(kept)
			if _, err := s.Write(day, family, product, kept); err != nil {
				return before, after, err
			}
		}
	}
	return before, after, nil
}

func readFile(fn string) ([]firms.Detection, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(fn, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("store: %s: %w", fn, err)
		}
		defer gz.Close()
		r = gz
	}
	recs, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("store: %s: %w", fn, err)
	}
	if len(recs) == 0 {
		return nil, nil
	}
	if strings.Join(recs[0], ",") != strings.Join(header, ",") {
		return nil, fmt.Errorf("store: %s: unexpected header", fn)
	}
	out := make([]firms.Detection, 0, len(recs)-1)
	for i, r := range recs[1:] {
		lat, err1 := strconv.ParseFloat(r[0], 64)
		lon, err2 := strconv.ParseFloat(r[1], 64)
		t, err3 := time.Parse(time.RFC3339, r[2])
		frp, err4 := strconv.ParseFloat(r[7], 64)
		typ, err5 := strconv.Atoi(r[9])
		if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
			return nil, fmt.Errorf("store: %s line %d: %w", fn, i+2, err)
		}
		out = append(out, firms.Detection{
			Lat: lat, Lon: lon, Time: t.UTC(),
			Source: r[3], Satellite: r[4], Instrument: r[5],
			Confidence: firms.ParseConfidence(r[6]),
			FRP:        frp, DayNight: r[8], Type: typ,
		})
	}
	return out, nil
}
