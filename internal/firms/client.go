package firms

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://firms.modaps.eosdis.nasa.gov"

// MaxDayRange is the largest DAY_RANGE the FIRMS area API accepts.
const MaxDayRange = 5

// Client is a minimal FIRMS API client.
type Client struct {
	MapKey  string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(mapKey string) *Client {
	return &Client{
		MapKey:  mapKey,
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

// Area fetches detections for source within bbox for [start, start+days-1] (UTC dates).
func (c *Client) Area(ctx context.Context, source string, bbox BBox, start time.Time, days int) ([]Detection, error) {
	if days < 1 || days > MaxDayRange {
		return nil, fmt.Errorf("firms: day range %d outside 1..%d", days, MaxDayRange)
	}
	url := fmt.Sprintf("%s/api/area/csv/%s/%s/%s/%d/%s",
		c.BaseURL, c.MapKey, source, bbox, days, start.UTC().Format("2006-01-02"))
	body, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	return ParseCSV(bytes.NewReader(body), source)
}

// DateRange is an inclusive span of UTC dates.
type DateRange struct {
	Min, Max time.Time
}

func (r DateRange) Covers(day time.Time) bool {
	return !day.Before(r.Min) && !day.After(r.Max)
}

// Availability returns the date coverage of every FIRMS product, keyed by source name.
func (c *Client) Availability(ctx context.Context) (map[string]DateRange, error) {
	body, err := c.get(ctx, fmt.Sprintf("%s/api/data_availability/csv/%s/all", c.BaseURL, c.MapKey))
	if err != nil {
		return nil, err
	}
	return ParseAvailability(bytes.NewReader(body))
}

// ParseAvailability parses the data_availability CSV (data_id,min_date,max_date).
func ParseAvailability(r io.Reader) (map[string]DateRange, error) {
	cr := csv.NewReader(r)
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("firms: availability: %w", err)
	}
	if len(recs) < 1 || len(recs[0]) < 3 || strings.TrimSpace(recs[0][0]) != "data_id" {
		return nil, fmt.Errorf("%w: availability", ErrNotCSV)
	}
	out := make(map[string]DateRange, len(recs)-1)
	for _, rec := range recs[1:] {
		if len(rec) < 3 {
			continue
		}
		minD, err1 := time.Parse("2006-01-02", strings.TrimSpace(rec[1]))
		maxD, err2 := time.Parse("2006-01-02", strings.TrimSpace(rec[2]))
		if err1 != nil || err2 != nil {
			continue
		}
		out[strings.TrimSpace(rec[0])] = DateRange{Min: minD, Max: maxD}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "paraliwatch/0.1")
	var lastErr error
	wait := time.Duration(0)
	for attempt := 0; attempt < 6; attempt++ {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		wait = time.Duration(attempt+1) * 5 * time.Second
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("firms: HTTP %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK && isRateLimited(body) {
			// FIRMS signals its 5,000-transactions-per-10-minutes cap with HTTP 400.
			lastErr = fmt.Errorf("firms: rate limited: %s", firstLine(body))
			wait = RateLimitWait
			slog.Warn("FIRMS transaction limit hit; waiting", "for", wait)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("firms: HTTP %d: %s", resp.StatusCode, firstLine(body))
		}
		return body, nil
	}
	return nil, lastErr
}

// RateLimitWait is how long to back off after FIRMS reports the transaction cap
// (the cap is counted over a rolling 10-minute window).
var RateLimitWait = 3 * time.Minute

func isRateLimited(body []byte) bool {
	return bytes.Contains(bytes.ToLower(body), []byte("transaction limit"))
}

func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(bytes.TrimSpace(b), []byte("\n"))
	if len(line) > 200 {
		line = line[:200]
	}
	return string(line)
}

// Chunk is one API request: a product over a run of consecutive UTC dates.
type Chunk struct {
	Source string
	Start  time.Time
	Days   int
}

// Plan splits [from, to] (UTC dates, inclusive) into API requests for one
// family. Each day uses the Standard Processing (SP) product when available
// (it is the reprocessed, authoritative archive), else NRT. Days covered by
// neither are skipped. Runs are capped at MaxDayRange.
func Plan(family string, from, to time.Time, avail map[string]DateRange) []Chunk {
	from, to = truncDay(from), truncDay(to)
	pick := func(day time.Time) string {
		if r, ok := avail[family+"_SP"]; ok && r.Covers(day) {
			return family + "_SP"
		}
		if r, ok := avail[family+"_NRT"]; ok && r.Covers(day) {
			return family + "_NRT"
		}
		return ""
	}
	var out []Chunk
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		src := pick(day)
		if src == "" {
			continue
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.Source == src && last.Days < MaxDayRange && last.Start.AddDate(0, 0, last.Days).Equal(day) {
				last.Days++
				continue
			}
		}
		out = append(out, Chunk{Source: src, Start: day, Days: 1})
	}
	return out
}

func truncDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
