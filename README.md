# Parali Watch

**Live: https://technusm1.github.io/paraliwatch/** · Write-up: https://maheepk.net/posts/north-indias-annual-smoke-festival/ · Part of [File Pending](https://maheepk.net/projects/file-pending/).

Public dashboard of satellite-detected farm fires across Punjab, Haryana,
Uttar Pradesh, Rajasthan, Delhi and Uttarakhand, built from NASA FIRMS and updated
every 3 hours. Written in Go, published as a static site.

See [PLAN.md](PLAN.md) for the motivation, architecture and roadmap (geostationary
data, Sentinel-2 burnt area, air quality).

## Quick start

```sh
# 1. Free API key: https://firms.modaps.eosdis.nasa.gov/api/map_key/
export FIRMS_MAP_KEY=...

# 2. Pull detections (UTC dates). Backfill a season, or just the last few days.
go run ./cmd/paraliwatch fetch --from 2026-09-15 --to 2026-11-12
go run ./cmd/paraliwatch fetch --days 3

# 3. Interval report in the terminal (times are IST unless zoned)
go run ./cmd/paraliwatch report --from "2026-11-01 00:00" --to "2026-11-12 18:00"
go run ./cmd/paraliwatch report --from 2026-11-01 --to 2026-11-11 --min-conf high --json

# 4. Build and preview the site
go run ./cmd/paraliwatch build --out site
go run ./cmd/paraliwatch serve --dir site   # http://127.0.0.1:8080
```

Options: `--states "Punjab,Haryana"` limits coverage; `build --since 2024-01-01`
caps how much history is published.

## Deploy (GitHub Pages)

1. Push this repo to GitHub.
2. Settings → Secrets and variables → Actions → add `FIRMS_MAP_KEY`.
3. Settings → Pages → Source: **GitHub Actions**.
4. Actions → `update` → Run workflow with `from=2020-09-15` to backfill.
   After that it runs every 3 hours on its own.

## Layout

```
cmd/paraliwatch/      CLI: fetch, report, build, serve
internal/firms/       FIRMS API client, CSV parsing, SP/NRT request planning
internal/geo/         embedded district boundaries, point-in-polygon locator
internal/store/       raw CSV partitions: data/fires/YYYY-MM-DD/<FAMILY>.<SP|NRT>.csv
internal/report/      interval filtering + aggregation (IST)
internal/site/        static site builder + embedded web assets
data/fires/           raw detections (committed by CI)
```

## Data and credits

Fire data: NASA FIRMS (LANCE / EOSDIS). Boundaries: Census-2011 districts via
[udit-001/india-maps-data](https://github.com/udit-001/india-maps-data). Basemap:
Esri Light/Dark Gray Canvas.
