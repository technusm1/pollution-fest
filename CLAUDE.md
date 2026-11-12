# Parali Watch

Public dashboard of satellite-detected farm fires (stubble burning) in Punjab, Haryana,
Uttar Pradesh, Rajasthan, Delhi and Uttarakhand. Go CLI + static site on GitHub Pages.
The aim is public accountability, so accuracy and honest caveats matter more than drama.
Roadmap and rationale: `PLAN.md` (keep its phase checkboxes current when work lands).

## Commands

```sh
set -a; source .env; set +a                     # FIRMS_MAP_KEY (gitignored; never commit it)
go test ./...
go run ./cmd/paraliwatch fetch --from 2026-09-15     # or --days 3; UTC dates; idempotent
go run ./cmd/paraliwatch report --from 2026-11-01 --to 2026-11-12   # IST; date-only --to = whole day
go run ./cmd/paraliwatch build --out site
go run ./cmd/paraliwatch serve --dir site            # 127.0.0.1:8080
go run ./cmd/paraliwatch compact                     # gzip + drop rows outside states/extras
```

## Layout

- `cmd/paraliwatch/`: CLI (fetch, report, build, serve)
- `internal/firms/`: FIRMS area + data_availability API, CSV parsing (VIIRS/MODIS), `Plan()` picks SP vs NRT per day, ≤5-day chunks
- `internal/geo/`: embedded Census-2011 district GeoJSON (`boundaries/`), point-in-polygon via `paulmach/orb`. `Keeper` = which raw rows to store: the six states + `ExtraRegions` (default `pakistan-punjab`, geoBoundaries public domain), ~2 km tolerance
- `internal/store/`: raw partitions `data/fires/YYYY-MM-DD/<FAMILY>.<SP|NRT>.csv.gz` (UTC date; gzip with zeroed mtime so identical data means identical bytes; legacy `.csv` still readable). `Compact` rewrites + filters in place. Each write replaces the partition; SP is never downgraded to NRT
- `internal/report/`: `Locate` (drops out-of-area + FIRMS type 2/3 static sources), `Filter`, `Summarize`, all in IST
- `internal/site/`: builds `site/data/{manifest.json,districts.geojson,days/<IST date>.json}` + embedded `web/` (Leaflet 1.9.4, Chart.js 4.4.1 from cdnjs). Browser does all interval filtering
- `.github/workflows/update.yml`: cron every 3h: test → fetch --days 3 → commit data → build → Pages

## Conventions

- Go, standard library first; `paulmach/orb` is the only dependency. Imports at top of file, never inside functions.
- Times: store UTC; present IST (fixed +05:30). Store partitions are UTC dates; site day files are IST dates.
- "Detections" ≠ fires ≠ burnt area. Keep that wording on the site and in reports.
- Chart colours follow the dataviz palette: states take fixed slots 1–6 in order Punjab, Haryana, UP, Rajasthan, Delhi, Uttarakhand; the choropleth uses the sequential blue ramp. Colour tokens live in `web/style.css` with light and dark variants.
- Verify claims against real data before putting them on the site (e.g. the after-3 PM share is ~18–30%, not ~0%, because Aqua's orbit has drifted; see below).
- Every satirical claim must be real and linked: bingo tiles (`EXCUSES` in `web/app.js`) are `[text, sourceURL]` pairs of things an official actually said or did; timeline items and badges link to sources; effect cards cite and link. No invented quotes. Check that each URL loads (some sites return 403 to curl; swap those for an alternative). HTTP 200 is not proof: Down To Earth silently redirects dead articles to its homepage, so check the final URL isn't `/`, and confirm the cited figure actually appears on the page (use the cmux browser for JS-rendered sites). Don't name a shell variable `path` in zsh loops (it clobbers PATH).
- Time-sensitive figures (AQI etc.) carry an explicit datetime and source, never "this week".
- Never add Claude attribution to commits.

## Data facts worth remembering

- FIRMS area API: `DAY_RANGE` 1–5, bbox `west,south,east,north`, 5,000 transactions / 10 min per key; a multi-day request counts as several. Over the cap it returns HTTP 400 "Exceeding allowed transaction limit"; the client waits `RateLimitWait` (3 min) and retries.
- MODIS Aqua's orbit has drifted: by 2026 it crosses Punjab at about 15:00–16:30 IST, which explains nearly all post-3 PM detections. NOAA-20 shows 0% after 3 PM in every year. FIRMS sometimes returns errors as HTTP 200 text (`ErrNotCSV`).
- SP (standard processing) lags about 3–4 months; NRT covers recent dates. `data_availability` decides which to use.
- **S-NPP VIIRS delivery ended 2026-11-02.** Normalise per satellite before comparing years.
- FIRMS also offers `BA_VIIRS` / `BA_MODIS` burnt-area products (about 3 months lag). Look at these before building our own Phase 4 pipeline.
- Basemap: Esri Light/Dark Gray Canvas (CARTO now needs a key). It does not follow Survey of India borders; replace before wide promotion.
- Boundary source `udit-001/india-maps-data` has no license. Resolve before going public.

## Status (2026-11-12)

- Phase 1 built and verified on real data. Backfilled 2020-01-01 → 2026-11-12, all four families, no gaps. Raw data compacted 2026-11-12: 4.88M → 3.09M rows (six states + Pakistani Punjab), 390 MB → 56 MB gzip; six-state report totals verified identical before/after.
- Coverage gap: history before 2026-11-12 used the old bbox (north edge ~32.65°N), so northern Pakistani Punjab (Rawalpindi/Attock/Jhelum, ≥32.65°N) is missing from the backfill; new fetches cover it. Fixing it means a full re-fetch (Write replaces whole partitions, so a strip-only fetch would wipe data).
- Site has a satirical layer: hero + Golden Matchstick, "Festival highlights" effect cards, YoY (NOAA-20 only), count-vs-burnt-area gap (iFOREST), Aqua-drift explainer, rulers strip, linked timeline with Union/State/Court badges and emoji party symbols, excuse bingo (26 sourced tiles with ↗ links, "Shuffle & reset"), "Before you blame the farmer", "So what should Punjab grow?". Keep it non-partisan in tone (every party gets the same treatment) and sourced.
- All 76 external links audited 2026-11-12 (status, homepage redirect, soft-404). Re-run the audit after content changes.
- Session handoff (pending decisions, next ideas) lives in Claude memory for this folder: `session-handoff`.
- **Live since 2026-11-12:** public repo https://github.com/technusm1/paraliwatch, dashboard https://technusm1.github.io/paraliwatch/ (Pages via Actions; `update.yml` runs every 3h with the `FIRMS_MAP_KEY` secret and commits data as paraliwatch-bot, so `git pull --rebase` before pushing). Blog: https://maheepk.net/projects/file-pending/ (project), /projects/file-pending/parali-watch/ (project page), /posts/north-indias-annual-smoke-festival/ (post).
