// Command paraliwatch ingests NASA FIRMS farm-fire detections for north India
// and builds a static public dashboard.
//
//	paraliwatch fetch  --from 2026-10-01 --to 2026-11-12     # or --days 3
//	paraliwatch report --from "2026-11-01 00:00" --to "2026-11-12 23:59"
//	paraliwatch build  --out site
//	paraliwatch serve  --dir site
//	paraliwatch compact                                      # gzip + drop out-of-region rows
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"paraliwatch/internal/firms"
	"paraliwatch/internal/geo"
	"paraliwatch/internal/report"
	"paraliwatch/internal/site"
	"paraliwatch/internal/store"
)

const usage = `paraliwatch - track north-Indian farm fires from NASA FIRMS

Usage:
  paraliwatch fetch  [--from DATE --to DATE | --days N] [--data DIR] [--states LIST] [--force]
  paraliwatch report  --from TIME --to TIME [--min-conf low|nominal|high] [--states LIST] [--top N] [--json]
  paraliwatch build  [--out DIR] [--data DIR] [--since DATE] [--states LIST]
  paraliwatch serve  [--dir DIR] [--addr :8080]
  paraliwatch compact [--data DIR] [--states LIST] [--extras LIST]

Raw data keeps detections in --states plus --extras regions (default: pakistan-punjab).

Times without a zone are IST. A date-only --to includes that whole day.
fetch needs FIRMS_MAP_KEY (free: https://firms.modaps.eosdis.nasa.gov/api/map_key/).
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch os.Args[1] {
	case "fetch":
		err = cmdFetch(ctx, os.Args[2:])
	case "report":
		err = cmdReport(os.Args[2:])
	case "build":
		err = cmdBuild(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "compact":
		err = cmdCompact(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "paraliwatch:", err)
		os.Exit(1)
	}
}

func statesFlag(fs *flag.FlagSet) *string {
	return fs.String("states", strings.Join(geo.DefaultStates, ","), "comma-separated states")
}

func extrasFlag(fs *flag.FlagSet) *string {
	return fs.String("extras", strings.Join(geo.DefaultExtras, ","), "extra regions to keep in raw data (comma-separated; empty for none)")
}

func newKeeper(states, extras string) (*geo.Keeper, error) {
	loc, err := geo.NewLocator(splitList(states))
	if err != nil {
		return nil, err
	}
	return geo.NewKeeper(loc, splitList(extras))
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func cmdFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	from := fs.String("from", "", "first UTC date (YYYY-MM-DD)")
	to := fs.String("to", "", "last UTC date (YYYY-MM-DD), default today")
	days := fs.Int("days", 0, "fetch the last N UTC days (overrides --from)")
	dataDir := fs.String("data", "data/fires", "raw data directory")
	states := statesFlag(fs)
	extras := extrasFlag(fs)
	force := fs.Bool("force", false, "re-fetch days already stored as SP")
	_ = fs.Parse(args)

	key := os.Getenv("FIRMS_MAP_KEY")
	if key == "" {
		return errors.New("FIRMS_MAP_KEY is not set (get one free at https://firms.modaps.eosdis.nasa.gov/api/map_key/)")
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	end := today
	if *to != "" {
		t, err := time.Parse("2006-01-02", *to)
		if err != nil {
			return fmt.Errorf("--to: %w", err)
		}
		end = t
	}
	var start time.Time
	switch {
	case *days > 0:
		start = end.AddDate(0, 0, -(*days - 1))
	case *from != "":
		t, err := time.Parse("2006-01-02", *from)
		if err != nil {
			return fmt.Errorf("--from: %w", err)
		}
		start = t
	default:
		return errors.New("need --from or --days")
	}
	if end.Before(start) {
		return errors.New("--to is before --from")
	}

	keeper, err := newKeeper(*states, *extras)
	if err != nil {
		return err
	}
	b := keeper.Bound()
	bbox := firms.BBox{West: b.Min[0], South: b.Min[1], East: b.Max[0], North: b.Max[1]}

	client := firms.NewClient(key)
	avail, err := client.Availability(ctx)
	if err != nil {
		return fmt.Errorf("availability: %w", err)
	}
	st := store.Store{Root: *dataDir}
	slog.Info("fetch", "from", start.Format("2006-01-02"), "to", end.Format("2006-01-02"), "bbox", bbox.String())

	for _, fam := range firms.Families {
		for _, ch := range firms.Plan(fam, start, end, avail) {
			product := firms.Product(ch.Source)
			if product == "SP" && !*force && allStoredSP(st, fam, ch) {
				continue
			}
			dets, err := client.Area(ctx, ch.Source, bbox, ch.Start, ch.Days)
			if err != nil {
				return fmt.Errorf("%s %s+%d: %w", ch.Source, ch.Start.Format("2006-01-02"), ch.Days, err)
			}
			byDay := map[string][]firms.Detection{}
			kept := 0
			for _, d := range dets {
				if !keeper.Keep(d.Lat, d.Lon) {
					continue
				}
				kept++
				byDay[d.Time.Format("2006-01-02")] = append(byDay[d.Time.Format("2006-01-02")], d)
			}
			for i := 0; i < ch.Days; i++ {
				day := ch.Start.AddDate(0, 0, i)
				if _, err := st.Write(day, fam, product, byDay[day.Format("2006-01-02")]); err != nil {
					return err
				}
			}
			slog.Info("fetched", "source", ch.Source, "start", ch.Start.Format("2006-01-02"), "days", ch.Days, "detections", len(dets), "kept", kept)
		}
	}
	return nil
}

func allStoredSP(st store.Store, fam string, ch firms.Chunk) bool {
	for i := 0; i < ch.Days; i++ {
		if p, ok := st.Product(ch.Start.AddDate(0, 0, i), fam); !ok || p != "SP" {
			return false
		}
	}
	return true
}

// parseTime accepts RFC3339, "YYYY-MM-DD HH:MM", "YYYY-MM-DDTHH:MM" or
// "YYYY-MM-DD" (IST when no zone). endOfDay turns a bare date into the next
// midnight so the whole day is included.
func parseTime(s string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, report.IST); err == nil {
			return t, nil
		}
	}
	t, err := time.ParseInLocation("2006-01-02", s, report.IST)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse time %q", s)
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

func loadLocated(dataDir string, states []string, from, to time.Time) (*geo.Locator, []report.Located, error) {
	loc, err := geo.NewLocator(states)
	if err != nil {
		return nil, nil, err
	}
	// IST intervals straddle UTC dates; widen by a day on each side, Filter trims.
	dets, err := store.Store{Root: dataDir}.Load(from.AddDate(0, 0, -1), to.AddDate(0, 0, 1))
	if err != nil {
		return nil, nil, err
	}
	return loc, report.Locate(dets, loc), nil
}

func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	fromS := fs.String("from", "", "interval start (IST unless zoned)")
	toS := fs.String("to", "", "interval end, exclusive (date-only = whole day)")
	dataDir := fs.String("data", "data/fires", "raw data directory")
	states := statesFlag(fs)
	minConf := fs.String("min-conf", "nominal", "minimum confidence: low|nominal|high")
	top := fs.Int("top", 15, "districts to show")
	asJSON := fs.Bool("json", false, "print JSON")
	_ = fs.Parse(args)

	if *fromS == "" || *toS == "" {
		return errors.New("need --from and --to")
	}
	from, err := parseTime(*fromS, false)
	if err != nil {
		return err
	}
	to, err := parseTime(*toS, true)
	if err != nil {
		return err
	}
	conf := firms.ParseConfidence(*minConf)
	if conf == firms.ConfUnknown {
		return fmt.Errorf("bad --min-conf %q", *minConf)
	}
	_, located, err := loadLocated(*dataDir, splitList(*states), from, to)
	if err != nil {
		return err
	}
	s := report.Summarize(located, report.Filter{From: from, To: to, MinConf: conf})
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	printSummary(s, *top)
	return nil
}

func printSummary(s report.Summary, top int) {
	fmt.Printf("Interval  %s → %s (IST)\n", s.From.In(report.IST).Format("2006-01-02 15:04"), s.To.In(report.IST).Format("2006-01-02 15:04"))
	fmt.Printf("Detections %d   Total FRP %.0f MW\n", s.Total, s.FRP)
	if s.Total > 0 {
		fmt.Printf("Seen after 3 PM IST: %d (%.1f%%) — polar satellites barely look then; most large fires burn then.\n\n",
			s.AfterThreePM, 100*float64(s.AfterThreePM)/float64(s.Total))
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "State\tDetections\tFRP MW\t")
	for _, c := range s.ByState {
		fmt.Fprintf(w, "%s\t%d\t%.0f\t\n", c.Key, c.N, c.FRP)
	}
	w.Flush()
	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "District\tState\tDetections\t")
	for i, c := range s.ByDistrict {
		if i >= top {
			break
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t\n", c.Key, c.State, c.N)
	}
	w.Flush()
	fmt.Println()
	fmt.Println("Detections by hour (IST):")
	maxH := 0
	for _, n := range s.ByHourIST {
		maxH = max(maxH, n)
	}
	for h, n := range s.ByHourIST {
		if n == 0 {
			continue
		}
		bar := strings.Repeat("█", max(1, n*40/max(1, maxH)))
		fmt.Printf("  %02d:00  %-40s %d\n", h, bar, n)
	}
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "site", "output directory")
	dataDir := fs.String("data", "data/fires", "raw data directory")
	since := fs.String("since", "2020-01-01", "earliest IST date to publish")
	states := statesFlag(fs)
	_ = fs.Parse(args)

	from, err := parseTime(*since, false)
	if err != nil {
		return err
	}
	to := time.Now().Add(24 * time.Hour)
	loc, located, err := loadLocated(*dataDir, splitList(*states), from, to)
	if err != nil {
		return err
	}
	kept := located[:0]
	for _, l := range located {
		if !l.Time.Before(from) {
			kept = append(kept, l)
		}
	}
	if err := site.Build(*out, loc, kept, time.Now()); err != nil {
		return err
	}
	slog.Info("built site", "out", *out, "detections", len(kept))
	return nil
}

func cmdCompact(args []string) error {
	fs := flag.NewFlagSet("compact", flag.ExitOnError)
	dataDir := fs.String("data", "data/fires", "raw data directory")
	states := statesFlag(fs)
	extras := extrasFlag(fs)
	_ = fs.Parse(args)

	keeper, err := newKeeper(*states, *extras)
	if err != nil {
		return err
	}
	before, after, err := store.Store{Root: *dataDir}.Compact(func(d firms.Detection) bool {
		return keeper.Keep(d.Lat, d.Lon)
	})
	if err != nil {
		return err
	}
	slog.Info("compacted", "rows_before", before, "rows_after", after)
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", "site", "site directory")
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	_ = fs.Parse(args)
	slog.Info("serving", "url", "http://"+*addr)
	return http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir)))
}
