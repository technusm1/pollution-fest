# Pollution Fest: project plan

## Why

Northern India's post-monsoon smog has a large stubble-burning component, but the
official yardstick (satellite fire **counts**) is broken:

- Polar-orbiting satellites (VIIRS on S-NPP/NOAA-20/NOAA-21, MODIS on Aqua/Terra)
  pass over Punjab between roughly **12:00 and 16:30 IST** by day (depending on where Punjab falls in the swath), and not at all in the evening. Farmers have moved
  burning to late afternoon and evening (ISRO SAC, *Current Science*, Nov 2025).
  iFOREST found **>90% of large fires in 2024–25 burned after 3 PM**.
- Haryana's official fire count fell from **1,406 to 662** (2024 → 2025), while
  satellite-estimated **burnt area rose from 7,117 to 8,812 km²** (iFOREST).
  Punjab's count fell 53%; its burnt area fell only 15%.
- Two years after the Supreme Court (22 Nov 2024) stressed that "the extent of fire
  can be demonstrated only by a burnt area", there is still **no finalised
  burnt-area protocol**. IARI's RTI reply says it is "only researching" one
  ([Indian Express, 12 Nov 2026](https://indianexpress.com/article/cities/delhi/stubble-burning-air-pollution-supreme-court-protocol-burnt-area-farm-fires-10959536/)).

Goal: a public, transparent, reproducible dashboard. It starts with what is easy
(fire detections, updated within hours) and builds toward what matters (burnt area
and the count-vs-area gap).

**Coverage:** Punjab, Haryana, Uttar Pradesh, Rajasthan, Delhi, Uttarakhand
(configurable via `--states`).

## Architecture

```
GitHub Actions (cron, every 3h)
  └─ pollution-fest fetch   NASA FIRMS area API → data/fires/YYYY-MM-DD/<FAMILY>.<SP|NRT>.csv  (committed)
  └─ pollution-fest build   point-in-polygon → districts → site/data/days/<IST date>.json + manifest
  └─ GitHub Pages        static site: Leaflet + Chart.js, all interval filtering client-side
```

- **Go, standard library first**; only dependency is `paulmach/orb` (GeoJSON + planar geometry).
- **Raw data is plain CSV in git.** Anyone can audit or reuse it, and the history is a public log.
- **SP over NRT.** Each UTC day/satellite partition holds either the near-real-time
  product or, once NASA publishes it, the reprocessed Standard Processing product.
  SP replaces NRT and is never downgraded.
- **Interval = any start/end datetime (IST).** The static site loads only the per-day
  files the interval touches and filters in the browser. The CLI `report` command
  does the same in the terminal.

## Phases

### Phase 1: Fire detections MVP ✅ (this commit)
- [x] FIRMS client: area API, data-availability, SP/NRT planning, 5-day chunking, retries
- [x] District/state locator from embedded Census-2011 district boundaries
- [x] Idempotent CSV store with SP precedence
- [x] `report` CLI: interval summary, top districts, hour-of-day histogram (IST)
- [x] Static dashboard: interval + preset picker, state and confidence filters, map
      (points + district choropleth), daily stacked bars by state, "blind spot"
      hour chart, worst-districts table, shareable URL hash, light/dark
- [x] GitHub Actions: 3-hourly fetch → commit → build → Pages; manual backfill input
- [ ] **Go live:** create the GitHub repo, add the `FIRMS_MAP_KEY` secret, enable Pages
      (Source: GitHub Actions), run a backfill from 2020-09-15 to today

### Phase 2: Credibility hardening
- [ ] **Persistent-hotspot mask** for NRT data (refineries, thermal plants, brick
      kilns), built from multi-year SP `type=2` pixels plus repeat-detection clustering,
      so recent data gets the same filter as archived data
- [ ] **De-duplicated "fire events"** alongside raw detections: cluster detections
      within ~375 m and a few hours across satellites
- [ ] **Density view:** detections per 1,000 km² of district area (or per 1,000 ha of paddy). Raw counts make big desert districts like Bikaner look worse than they are.
- [ ] **Year-over-year view:** same calendar window, 2020–2026, per state and district
- [ ] **Official comparison:** scrape IARI CREAMS daily bulletins and plot official
      counts next to ours
- [ ] S-NPP VIIRS delivery ended 2 Nov 2026: counts from Nov 2026 onward come from one fewer VIIRS satellite. Normalise per satellite when comparing years, or the drop will look like less burning.
- [ ] Newer district boundaries (LGD / post-2011 districts such as Malerkotla)
- [ ] Map boundary review: switch to a basemap and boundaries that follow Survey of
      India lines before wide public promotion (Indian mapping rules)

### Phase 3: Close the afternoon blind spot (geostationary)
- [ ] **INSAT-3DR / INSAT-3DS fire product** from ISRO MOSDAC (registration; ~30 min
      cadence; coarse ~4 km pixels)
- [ ] **Meteosat IODC FRP-PIXEL** from EUMETSAT LSA SAF (~15 min cadence; ~3–4 km
      pixels over India)
- [ ] Show "afternoon/evening fire activity" as a separate layer. Coarse pixels mean
      it is a **signal, not a field count**. Put polar vs geostationary hour-of-day
      histograms side by side: that chart *is* the evasion story.
- [ ] Explore geostationary latency: target <1 h end to end

### Phase 4: Burnt area (the metric that matters)
- [ ] Sentinel-2 L2A dNBR (before/after Normalised Burn Ratio) over paddy-growing
      districts, every ~5 days, via the Copernicus Data Space Ecosystem
      (Sentinel Hub Statistical API: plain HTTPS, callable from Go)
- [ ] Paddy mask (ESA WorldCover cropland ∩ Sentinel-1/2 paddy classification) so
      only paddy fields count
- [ ] Per-district burnt hectares per season; validate against published iFOREST and
      IARI figures (e.g. 2018–21: 49.1% of Punjab's paddy area burnt, 17.7% of Haryana's)
- [ ] **Headline metric: "Evasion gap" = burnt area ÷ fire detections**, per district per
      year. Rising gap = more burning hidden from the official count.
- [ ] Publish methodology plus open data so the result is reproducible and citable

### Phase 5: Impact on air
- [ ] Hourly PM2.5 for Delhi-NCR and Punjab cities (CPCB CAAQMS via OpenAQ v3)
- [ ] Replace hand-written AQI figures on the site (e.g. "376 at 4 PM on Diwali, 8 Nov 2026") with a live, timestamped CPCB/OpenAQ reading, and track missing-station gaps (125 readings went blank around Diwali 2026)
- [ ] Wind/transport: Open-Meteo or ERA5; HYSPLIT back-trajectories from Delhi
- [ ] IITM/CAQM Decision Support System stubble-contribution % where available
- [ ] Lagged correlation: upwind fire activity → Delhi PM2.5

### Phase 6: The way out (draft on branch `solution-research`, see `docs/solution.md`)
Rule: "the sun, not the wind". Make the better crop the farmer's own best choice; no policing.
- [x] Research notes, sourced (`docs/research/`): maize economics, groundwater and history, tried schemes, private demand, ethanol feedstock
- [x] Draft case: feed Punjab's distilleries with Punjab's kharif maize
- [x] "Should I switch?" calculator (CACP Punjab costs), draft
- [ ] Fuel-rice ledger: FCI rice to ethanol by year, sale price vs economic cost, implied loss
- [ ] Distillery × groundwater map (plant capacity over CGWB block categories)
- [ ] "Everyone agrees" timeline: who proposed it vs what happened
- [ ] Reconcile live-site claims with the new research (maize returns, 110 vs 111 blocks)

## Known limitations (also shown on the site)
- Detections ≠ fires ≠ burnt area. Multiple satellites can see the same fire.
- Small or short fires below sensor detection limits are missed; haze and cloud hide fires.
- NRT data may include industrial heat sources until the Phase 2 mask lands.
- Census-2011 district lines: newer districts are counted inside their parent.

## Data sources
| Source | Use | Access |
|---|---|---|
| NASA FIRMS (VIIRS 375 m, MODIS 1 km) | Active-fire detections | Free MAP_KEY, 5,000 req / 10 min |
| udit-001/india-maps-data (Census 2011) | District boundaries | Embedded GeoJSON |
| ISRO MOSDAC (INSAT-3DR/3DS) | Geostationary fires | Registration |
| EUMETSAT LSA SAF (Meteosat IODC) | Geostationary FRP | Registration |
| Copernicus Data Space (Sentinel-2) | Burnt area | Free account |
| OpenAQ / CPCB | Air quality | Free key |
