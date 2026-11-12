package firms

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ErrNotCSV is returned when FIRMS answers with an error message instead of CSV
// (it often does so with HTTP 200, e.g. "Invalid MAP_KEY.").
var ErrNotCSV = errors.New("firms: response is not detection CSV")

// ParseCSV parses a FIRMS area CSV (VIIRS or MODIS layout) into detections.
// source is the product the request asked for; it is stamped on each row.
func ParseCSV(r io.Reader, source string) ([]Detection, error) {
	br := bufio.NewReader(r)
	peek, _ := br.Peek(512)
	trimmed := bytes.TrimSpace(peek)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if !bytes.HasPrefix(trimmed, []byte("latitude")) {
		line, _, _ := bytes.Cut(trimmed, []byte("\n"))
		return nil, fmt.Errorf("%w: %q", ErrNotCSV, string(line))
	}

	cr := csv.NewReader(br)
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("firms: read header: %w", err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	for _, req := range []string{"latitude", "longitude", "acq_date", "acq_time", "confidence", "frp"} {
		if _, ok := col[req]; !ok {
			return nil, fmt.Errorf("firms: missing column %q", req)
		}
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	var out []Detection
	line := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			return nil, fmt.Errorf("firms: line %d: %w", line, err)
		}
		lat, err1 := strconv.ParseFloat(get(rec, "latitude"), 64)
		lon, err2 := strconv.ParseFloat(get(rec, "longitude"), 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("firms: line %d: bad coordinates", line)
		}
		t, err := ParseAcq(get(rec, "acq_date"), get(rec, "acq_time"))
		if err != nil {
			return nil, fmt.Errorf("firms: line %d: %w", line, err)
		}
		frp, _ := strconv.ParseFloat(get(rec, "frp"), 64)
		typ := -1
		if v := get(rec, "type"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				typ = n
			}
		}
		out = append(out, Detection{
			Lat:        lat,
			Lon:        lon,
			Time:       t,
			Source:     source,
			Satellite:  get(rec, "satellite"),
			Instrument: get(rec, "instrument"),
			Confidence: ParseConfidence(get(rec, "confidence")),
			FRP:        frp,
			DayNight:   get(rec, "daynight"),
			Type:       typ,
		})
	}
	return out, nil
}

// ParseAcq combines FIRMS acq_date (YYYY-MM-DD) and acq_time (HHMM UTC, leading
// zeros sometimes dropped) into a UTC time.
func ParseAcq(date, hhmm string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad acq_date %q", date)
	}
	n, err := strconv.Atoi(hhmm)
	if err != nil || n < 0 || n > 2359 || n%100 > 59 {
		return time.Time{}, fmt.Errorf("bad acq_time %q", hhmm)
	}
	return d.Add(time.Duration(n/100)*time.Hour + time.Duration(n%100)*time.Minute).UTC(), nil
}
