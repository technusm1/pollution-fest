"use strict";
// Parali Watch frontend. All filtering happens here, so any IST interval works
// against static per-day JSON files.

const IST_OFFSET_MS = 5.5 * 3600 * 1000;
const STATE_ORDER = ["Punjab", "Haryana", "Uttar Pradesh", "Rajasthan", "Delhi", "Uttarakhand"];
const MAX_POINTS = 60000;
const COL = { lat: 0, lon: 1, t: 2, frp: 3, conf: 4, src: 5, dist: 6 };

const state = {
  manifest: null,
  districtsGeo: null,
  dayCache: new Map(),
  map: null, pointLayer: null, choroLayer: null,
  charts: {},
  rows: [],
};

const $ = (id) => document.getElementById(id);
const css = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
const fmt = (n) => n.toLocaleString("en-IN");

// ---- time helpers (IST is fixed UTC+5:30, no DST) ----
function istInputToUnix(v) {
  const [d, t] = v.split("T");
  const [y, m, day] = d.split("-").map(Number);
  const [hh, mm] = (t || "00:00").split(":").map(Number);
  return (Date.UTC(y, m - 1, day, hh, mm) - IST_OFFSET_MS) / 1000;
}
function unixToIstInput(u) {
  return new Date(u * 1000 + IST_OFFSET_MS).toISOString().slice(0, 16);
}
function unixToIstDate(u) {
  return new Date(u * 1000 + IST_OFFSET_MS).toISOString().slice(0, 10);
}
function istHour(u) {
  return new Date(u * 1000 + IST_OFFSET_MS).getUTCHours();
}
function istDatesBetween(fromU, toU) {
  const out = [];
  let d = new Date(unixToIstDate(fromU) + "T00:00:00Z");
  const end = unixToIstDate(toU - 1);
  while (d.toISOString().slice(0, 10) <= end) {
    out.push(d.toISOString().slice(0, 10));
    d = new Date(d.getTime() + 86400000);
  }
  return out;
}

function stateColor(name) {
  const i = STATE_ORDER.indexOf(name);
  return css(`--series-${i >= 0 ? i + 1 : 1}`);
}

// ---- data loading ----
async function loadJSON(url) {
  const r = await fetch(url, { cache: "no-cache" });
  if (!r.ok) throw new Error(`${url}: HTTP ${r.status}`);
  return r.json();
}

async function loadDays(dates) {
  const available = new Set(state.manifest.days.map((d) => d.date));
  const need = dates.filter((d) => available.has(d) && !state.dayCache.has(d));
  await Promise.all(need.map(async (d) => state.dayCache.set(d, await loadJSON(`data/days/${d}.json`))));
  return dates.flatMap((d) => state.dayCache.get(d) || []);
}

// ---- filters & URL ----
function readFilters() {
  const states = new Set([...document.querySelectorAll("#states input:checked")].map((i) => i.value));
  return {
    from: istInputToUnix($("from").value),
    to: istInputToUnix($("to").value),
    conf: Number($("conf").value),
    states,
  };
}

function writeHash(f) {
  const p = new URLSearchParams({
    from: unixToIstInput(f.from), to: unixToIstInput(f.to), conf: String(f.conf),
    states: [...f.states].join(","),
  });
  history.replaceState(null, "", "#" + p.toString());
}

function applyPreset(name) {
  const last = state.manifest.last_detection ? Date.parse(state.manifest.last_detection) / 1000 : Date.now() / 1000;
  const endDay = istInputToUnix(unixToIstDate(last) + "T00:00") + 86400;
  let from;
  switch (name) {
    case "24h": from = last - 86400; $("to").value = unixToIstInput(Math.ceil(last / 60) * 60 + 60); $("from").value = unixToIstInput(Math.floor(from / 60) * 60); return;
    case "7d": from = endDay - 7 * 86400; break;
    case "30d": from = endDay - 30 * 86400; break;
    case "season": {
      // Kharif paddy-residue season: 15 Sep - 30 Nov of the latest data year.
      const y = unixToIstDate(last).slice(0, 4);
      from = istInputToUnix(`${y}-09-15T00:00`);
      $("from").value = unixToIstInput(from);
      $("to").value = unixToIstInput(istInputToUnix(`${y}-12-01T00:00`));
      return;
    }
  }
  $("from").value = unixToIstInput(from);
  $("to").value = unixToIstInput(endDay);
}

// ---- rendering ----
function filterRows(raw, f) {
  const d = state.manifest.districts;
  return raw.filter((r) =>
    r[COL.t] >= f.from && r[COL.t] < f.to &&
    (r[COL.conf] === 0 || r[COL.conf] >= f.conf) &&
    f.states.has(d[r[COL.dist]].state));
}

function renderTiles(rows) {
  const frp = rows.reduce((s, r) => s + r[COL.frp], 0);
  const byDist = new Map();
  let after3 = 0;
  for (const r of rows) {
    byDist.set(r[COL.dist], (byDist.get(r[COL.dist]) || 0) + 1);
    if (istHour(r[COL.t]) >= 15) after3++;
  }
  $("k-total").textContent = fmt(rows.length);
  $("k-frp").textContent = fmt(Math.round(frp)) + " MW";
  $("k-districts").textContent = fmt(byDist.size);
  let top = null;
  for (const [k, v] of byDist) if (!top || v > top[1]) top = [k, v];
  $("k-top").textContent = top ? `Most: ${state.manifest.districts[top[0]].name} (${fmt(top[1])})` : " ";
  $("k-after3").textContent = rows.length ? `${((100 * after3) / rows.length).toFixed(1)}%` : "–";
  return byDist;
}

function choroColor(n, breaks) {
  if (!n) return null;
  let i = breaks.findIndex((b) => n <= b);
  if (i < 0) i = breaks.length - 1;
  return css(`--seq-${i + 1}`);
}

function quantileBreaks(values) {
  const v = values.filter((x) => x > 0).sort((a, b) => a - b);
  if (!v.length) return [1, 2, 3, 4, 5, 6, 7];
  const q = (p) => v[Math.min(v.length - 1, Math.floor(p * v.length))];
  const raw = [q(1 / 7), q(2 / 7), q(3 / 7), q(4 / 7), q(5 / 7), q(6 / 7), v[v.length - 1]];
  for (let i = 1; i < raw.length; i++) raw[i] = Math.max(raw[i], raw[i - 1] + 1);
  return raw;
}

function renderMap(rows, byDist) {
  const breaks = quantileBreaks([...byDist.values()]);
  const surface = css("--surface-1");
  if (state.choroLayer) state.choroLayer.remove();
  state.choroLayer = L.geoJSON(state.districtsGeo, {
    style: (feat) => {
      const n = byDist.get(feat.properties.id) || 0;
      const c = choroColor(n, breaks);
      return { color: css("--axis"), weight: 0.6, fillColor: c || surface, fillOpacity: c ? 0.75 : 0.15 };
    },
    onEachFeature: (feat, layer) => {
      const n = byDist.get(feat.properties.id) || 0;
      layer.bindTooltip(`<strong>${feat.properties.district}</strong>, ${feat.properties.state}<br>${fmt(n)} detections`, { sticky: true });
    },
  });
  if ($("show-choro").checked) state.choroLayer.addTo(state.map);

  if (state.pointLayer) state.pointLayer.remove();
  state.pointLayer = L.layerGroup();
  const fire = css("--fire");
  const ring = css("--surface-1");
  const shown = rows.length > MAX_POINTS ? rows.slice(-MAX_POINTS) : rows;
  for (const r of shown) {
    const radius = Math.max(2.5, Math.min(7, 1.5 + Math.sqrt(r[COL.frp]) * 0.7));
    const m = L.circleMarker([r[COL.lat], r[COL.lon]], {
      radius, color: ring, weight: 0.75, fillColor: fire, fillOpacity: 0.8,
    });
    const d = state.manifest.districts[r[COL.dist]];
    m.bindTooltip(
      `<strong>${d.name}</strong>, ${d.state}<br>${unixToIstInput(r[COL.t]).replace("T", " ")} IST<br>` +
      `${r[COL.frp]} MW · ${state.manifest.sources[r[COL.src]].replace("_", " ")}`);
    state.pointLayer.addLayer(m);
  }
  if ($("show-points").checked) state.pointLayer.addTo(state.map);

  const ramp = [1, 2, 3, 4, 5, 6, 7].map((i) => `<span style="background:${css(`--seq-${i}`)}"></span>`).join("");
  $("map-legend").innerHTML =
    `<span class="item"><span class="swatch" style="background:${fire};border-radius:50%"></span>Fire detection (size = FRP)</span>` +
    `<span class="item">District detections: 1 <span class="ramp">${ramp}</span> ${fmt(breaks[breaks.length - 1])}</span>` +
    (rows.length > MAX_POINTS ? `<span class="item">Showing latest ${fmt(MAX_POINTS)} of ${fmt(rows.length)} points</span>` : "");
}

function chartDefaults() {
  Chart.defaults.font.family = 'system-ui, -apple-system, "Segoe UI", sans-serif';
  Chart.defaults.color = css("--text-secondary");
  Chart.defaults.borderColor = css("--grid");
}

function renderByDay(rows, f) {
  const dates = istDatesBetween(f.from, f.to);
  const states = STATE_ORDER.filter((s) => f.states.has(s) && state.manifest.states.includes(s));
  const idx = new Map(dates.map((d, i) => [d, i]));
  const series = new Map(states.map((s) => [s, new Array(dates.length).fill(0)]));
  for (const r of rows) {
    const s = state.manifest.districts[r[COL.dist]].state;
    const i = idx.get(unixToIstDate(r[COL.t]));
    if (i !== undefined && series.has(s)) series.get(s)[i]++;
  }
  const surface = css("--surface-1");
  const data = {
    labels: dates.map((d) => d.slice(5)),
    datasets: states.map((s) => ({
      label: s, data: series.get(s), backgroundColor: stateColor(s),
      borderColor: surface, borderWidth: { top: 2, bottom: 0, left: 0, right: 0 }, borderSkipped: false,
      borderRadius: 4, maxBarThickness: 24,
    })),
  };
  $("day-legend").innerHTML = states.map((s) =>
    `<span class="item"><span class="swatch" style="background:${stateColor(s)}"></span>${s}</span>`).join("");
  if (state.charts.day) state.charts.day.destroy();
  state.charts.day = new Chart($("by-day"), {
    type: "bar", data,
    options: {
      maintainAspectRatio: false, animation: false,
      interaction: { mode: "index", intersect: false },
      plugins: { legend: { display: false }, tooltip: { filter: (i) => i.raw > 0 } },
      scales: {
        x: { stacked: true, grid: { display: false }, ticks: { maxRotation: 0, autoSkipPadding: 12 } },
        y: { stacked: true, beginAtZero: true, border: { display: false }, ticks: { precision: 0 } },
      },
    },
  });
}

const blindSpotPlugin = {
  id: "blindSpot",
  beforeDatasetsDraw(chart) {
    const { ctx, chartArea, scales } = chart;
    const x0 = scales.x.getPixelForValue(15) - scales.x.width / 48;
    ctx.save();
    ctx.fillStyle = css("--grid");
    ctx.globalAlpha = 0.6;
    ctx.fillRect(x0, chartArea.top, chartArea.right - x0, chartArea.bottom - chartArea.top);
    ctx.globalAlpha = 1;
    ctx.fillStyle = css("--text-secondary");
    ctx.font = '12px system-ui, -apple-system, "Segoe UI", sans-serif';
    ctx.fillText("Peak burning (after 3 PM)", x0 + 6, chartArea.top + 14);
    ctx.restore();
  },
};

function renderByHour(rows) {
  const counts = new Array(24).fill(0);
  for (const r of rows) counts[istHour(r[COL.t])]++;
  if (state.charts.hour) state.charts.hour.destroy();
  state.charts.hour = new Chart($("by-hour"), {
    type: "bar",
    data: {
      labels: counts.map((_, h) => String(h).padStart(2, "0")),
      datasets: [{ label: "Detections", data: counts, backgroundColor: css("--accent"), borderRadius: 4, maxBarThickness: 18 }],
    },
    options: {
      maintainAspectRatio: false, animation: false,
      plugins: { legend: { display: false }, tooltip: { callbacks: { title: (i) => `${i[0].label}:00–${i[0].label}:59 IST` } } },
      scales: {
        x: { grid: { display: false }, ticks: { maxRotation: 0, autoSkipPadding: 6 } },
        y: { beginAtZero: true, border: { display: false }, ticks: { precision: 0 } },
      },
    },
    plugins: [blindSpotPlugin],
  });
}

function renderTable(rows) {
  const agg = new Map();
  for (const r of rows) {
    const a = agg.get(r[COL.dist]) || { n: 0, frp: 0 };
    a.n++; a.frp += r[COL.frp];
    agg.set(r[COL.dist], a);
  }
  const top = [...agg.entries()].sort((a, b) => b[1].n - a[1].n).slice(0, 25);
  const tbody = $("districts").querySelector("tbody");
  tbody.innerHTML = top.length ? "" : `<tr><td colspan="5" class="muted">No detections in this interval.</td></tr>`;
  top.forEach(([id, a], i) => {
    const d = state.manifest.districts[id];
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${i + 1}</td><td></td><td><span class="swatch" style="background:${stateColor(d.state)}"></span> </td>` +
      `<td class="num">${fmt(a.n)}</td><td class="num">${fmt(Math.round(a.frp))}</td>`;
    tr.children[1].textContent = d.name;
    tr.children[2].append(d.state);
    tbody.appendChild(tr);
  });
}

async function refresh() {
  const f = readFilters();
  if (!(f.to > f.from)) { alert("'To' must be after 'From'."); return; }
  writeHash(f);
  const raw = await loadDays(istDatesBetween(f.from, f.to));
  const rows = filterRows(raw, f);
  state.rows = rows;
  const byDist = renderTiles(rows);
  renderMap(rows, byDist);
  renderByDay(rows, f);
  renderByHour(rows);
  renderTable(rows);
}

function initStates() {
  const box = $("states");
  for (const s of STATE_ORDER.filter((s) => state.manifest.states.includes(s))) {
    const label = document.createElement("label");
    label.innerHTML = `<input type="checkbox" value="${s}" checked><span class="swatch" style="background:${stateColor(s)}"></span>`;
    label.append(s);
    box.appendChild(label);
  }
}

function initFromHash() {
  const p = new URLSearchParams(location.hash.slice(1));
  if (p.get("from") && p.get("to")) {
    $("from").value = p.get("from");
    $("to").value = p.get("to");
  } else {
    applyPreset("7d");
  }
  if (p.get("conf")) $("conf").value = p.get("conf");
  if (p.has("states")) {
    const want = new Set(p.get("states").split(","));
    document.querySelectorAll("#states input").forEach((i) => { i.checked = want.has(i.value); });
  }
}

function initMap() {
  const dark = matchMedia("(prefers-color-scheme: dark)").matches && document.documentElement.dataset.theme !== "light"
    || document.documentElement.dataset.theme === "dark";
  state.map = L.map("map", { preferCanvas: true, zoomSnap: 0.25 }).setView([28.8, 77.5], 6);
  const base = dark ? "World_Dark_Gray_Base" : "World_Light_Gray_Base";
  L.tileLayer(`https://server.arcgisonline.com/ArcGIS/rest/services/Canvas/${base}/MapServer/tile/{z}/{y}/{x}`, {
    maxZoom: 16,
    attribution: "Basemap © Esri, HERE, Garmin, © OpenStreetMap contributors",
  }).addTo(state.map);
  const b = L.geoJSON(state.districtsGeo).getBounds();
  if (b.isValid()) state.map.fitBounds(b, { padding: [10, 10] });
  $("show-points").addEventListener("change", (e) => e.target.checked ? state.pointLayer?.addTo(state.map) : state.pointLayer?.remove());
  $("show-choro").addEventListener("change", (e) => e.target.checked ? state.choroLayer?.addTo(state.map) : state.choroLayer?.remove());
}

// ---- hero ----
async function renderHero() {
  const m = state.manifest;
  if (!m.last_detection) return;
  const last = Date.parse(m.last_detection) / 1000;
  const year = unixToIstDate(last).slice(0, 4);
  const seasonStart = istInputToUnix(`${year}-09-15T00:00`);
  const dists = m.districts;
  const raw = await loadDays(istDatesBetween(Math.min(seasonStart, last - 86400), last + 1));
  let season = 0, day = 0;
  const byDist = new Map();
  for (const r of raw) {
    if (r[COL.conf] !== 0 && r[COL.conf] < 2) continue;
    if (r[COL.t] >= seasonStart) season++;
    if (r[COL.t] > last - 86400) {
      day++;
      byDist.set(r[COL.dist], (byDist.get(r[COL.dist]) || 0) + 1);
    }
  }
  $("h-season").textContent = last >= seasonStart ? fmt(season) : "off-season";
  $("h-season-since").textContent = `(since 15 Sep ${year})`;
  $("h-24h").textContent = fmt(day);
  let top = null;
  for (const [k, v] of byDist) if (!top || v > top[1]) top = [k, v];
  if (top) {
    $("h-award").textContent = `${dists[top[0]].name}, ${dists[top[0]].state}`;
    $("h-award-note").textContent = `${fmt(top[1])} detections in the last 24 hours. Congratulations to all involved.`;
  } else {
    $("h-award").textContent = "Nobody 🎉";
    $("h-award-note").textContent = "No detections in the last 24 hours. Either a miracle or a cloudy day.";
  }
}

// ---- season-over-season ----
async function renderSeasons() {
  let s;
  try { s = await loadJSON("data/seasons.json"); } catch { return; }
  if (!s.years || !s.years.length) return;
  const [mm, dd] = s.cutoff.split(" ")[0].split("-").map(Number);
  const cutoffLabel = new Date(Date.UTC(2000, mm - 1, dd)).toLocaleDateString("en-IN", { day: "numeric", month: "short", timeZone: "UTC" });
  $("yoy-cutoff").textContent = cutoffLabel;
  const states = STATE_ORDER.filter((st) => state.manifest.states.includes(st));
  const surface = css("--surface-1");
  $("yoy-legend").innerHTML = states.map((st) =>
    `<span class="item"><span class="swatch" style="background:${stateColor(st)}"></span>${st}</span>`).join("");
  if (state.charts.yoy) state.charts.yoy.destroy();
  state.charts.yoy = new Chart($("yoy"), {
    type: "bar",
    data: {
      labels: s.years.map((y) => String(y.year)),
      datasets: states.map((st) => ({
        label: st, data: s.years.map((y) => y.to_date[st] || 0), backgroundColor: stateColor(st),
        borderColor: surface, borderWidth: { top: 2, bottom: 0, left: 0, right: 0 }, borderSkipped: false,
        borderRadius: 4, maxBarThickness: 48,
      })),
    },
    options: {
      maintainAspectRatio: false, animation: false,
      interaction: { mode: "index", intersect: false },
      plugins: { legend: { display: false }, tooltip: { filter: (i) => i.raw > 0, callbacks: { footer: (items) => `Total: ${fmt(items.reduce((a, i) => a + i.raw, 0))}` } } },
      scales: {
        x: { stacked: true, grid: { display: false } },
        y: { stacked: true, beginAtZero: true, border: { display: false }, ticks: { precision: 0 } },
      },
    },
  });
  const first = s.years[0], lastY = s.years[s.years.length - 1];
  const pb0 = first.to_date.Punjab || 0, pb1 = lastY.to_date.Punjab || 0;
  if (pb0 > 0 && first !== lastY) {
    const drop = Math.round(100 * (1 - pb1 / pb0));
    $("yoy-callout").innerHTML = drop > 0
      ? `Punjab's count is down <strong>${drop}%</strong> since ${first.year}. Pop the champagne? Not yet. Look right. ➡️`
      : `Punjab's count is <strong>up ${-drop}%</strong> since ${first.year}. So much for the plan.`;
  }
}

function renderGap() {
  // iFOREST (via Indian Express, 12 Nov 2026): 2024 → 2025.
  const rows = [
    { st: "Punjab", count: [10909, 5114], area: [23262, 19757] },
    { st: "Haryana", count: [1406, 662], area: [7117, 8812] },
  ];
  const pct = ([a, b]) => Math.round((100 * (b - a)) / a);
  new Chart($("gap"), {
    type: "bar",
    data: {
      labels: rows.map((r) => r.st),
      datasets: [
        { label: "Fires counted", data: rows.map((r) => pct(r.count)), backgroundColor: css("--series-1"), borderRadius: 4, maxBarThickness: 28 },
        { label: "Area burnt", data: rows.map((r) => pct(r.area)), backgroundColor: css("--series-2"), borderRadius: 4, maxBarThickness: 28 },
      ],
    },
    options: {
      indexAxis: "y", maintainAspectRatio: false, animation: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (i) => {
              const r = rows[i.dataIndex];
              const [a, b] = i.datasetIndex === 0 ? r.count : r.area;
              const unit = i.datasetIndex === 0 ? "" : " km²";
              return `${i.dataset.label}: ${fmt(a)}${unit} → ${fmt(b)}${unit} (${i.raw > 0 ? "+" : ""}${i.raw}%)`;
            },
          },
        },
      },
      scales: {
        x: { min: -60, max: 40, border: { display: false }, ticks: { callback: (v) => `${v > 0 ? "+" : ""}${v}%` },
             grid: { color: (c) => (c.tick.value === 0 ? css("--axis") : css("--grid")) } },
        y: { grid: { display: false } },
      },
    },
  });
}

// ---- who was in charge ----
// Fixed party order → categorical slots (colour follows the party, never its rank).
const PARTIES = {
  INC: { name: "Congress", sym: "✋", slot: 1 },
  BJP: { name: "BJP", sym: "🪷", slot: 2 },
  AAP: { name: "AAP", sym: "🧹", slot: 3 },
  SAD: { name: "SAD–BJP", sym: "⚖️", slot: 4 },
  SP: { name: "Samajwadi Party", sym: "🚲", slot: 5 },
};
const GANTT_FROM = 2015, GANTT_TO = 2027;
// [party, start (decimal year), end]. Sources: ECI results / swearing-in dates.
const RULERS = [
  ["Union (Centre)", [["BJP", 2015, 2027, "BJP-led NDA"]]],
  ["Punjab", [["SAD", 2015, 2017.21], ["INC", 2017.21, 2022.21], ["AAP", 2022.21, 2027]]],
  ["Haryana", [["BJP", 2015, 2027, "BJP (with JJP 2019–24)"]]],
  ["Delhi", [["AAP", 2015.12, 2025.14], ["BJP", 2025.14, 2027]]],
  ["Uttar Pradesh", [["SP", 2015, 2017.22], ["BJP", 2017.22, 2027]]],
  ["Rajasthan", [["BJP", 2015, 2018.96], ["INC", 2018.96, 2023.96], ["BJP", 2023.96, 2027]]],
  ["Uttarakhand", [["INC", 2015, 2017.22], ["BJP", 2017.22, 2027]]],
];

function renderGantt() {
  const span = GANTT_TO - GANTT_FROM;
  const pct = (y) => `${((Math.max(y, GANTT_FROM) - GANTT_FROM) / span) * 100}%`;
  const rows = RULERS.map(([gov, terms]) => {
    const segs = terms.map(([p, a, b, label]) => {
      const party = PARTIES[p];
      const w = ((Math.min(b, GANTT_TO) - Math.max(a, GANTT_FROM)) / span) * 100;
      const text = `${party.sym} ${label || party.name}`;
      const ink = party.slot === 4 ? "#0b0b0b" : "#fff";
      return `<div class="seg" style="left:${pct(a)};width:calc(${w}% - 2px);background:var(--series-${party.slot});color:${ink}" title="${gov}: ${text}, ${Math.floor(a)}–${b >= GANTT_TO ? "now" : Math.floor(b)}">
        <span>${w > 9 ? text : party.sym}</span></div>`;
    }).join("");
    return `<div class="g-row"><div class="g-label">${gov}</div><div class="g-track">${segs}</div></div>`;
  }).join("");
  const ticks = [];
  for (let y = GANTT_FROM; y <= GANTT_TO - 1; y += 2) ticks.push(`<span style="left:${pct(y)}">${y}</span>`);
  $("gantt").innerHTML = rows + `<div class="g-row g-axis"><div class="g-label"></div><div class="g-track">${ticks.join("")}</div></div>`;
  $("gantt-legend").innerHTML = Object.values(PARTIES).map((p) =>
    `<span class="item"><span class="swatch" style="background:var(--series-${p.slot})"></span>${p.sym} ${p.name}</span>`).join("");
}

// ---- excuse bingo ----
// Every tile is something a minister, official or government actually said or did.
// [text, source URL]. Keep it that way: no invented excuses.
const EXCUSES = [
  ["\u201cStubble is only 4% of Delhi's pollution\u201d", "https://theprint.in/environment/prakash-javadekar-says-only-4-pollution-in-delhi-ncr-due-to-stubble-burning/524102"],
  ["\u201cFarm fires are down 90%!\u201d", "https://healthpolicy-watch.news/scientist-disputes-indian-government-claim-that-polluting-farm-fires-have-reduced-by-90/"],
  ["\u201cPakistan or China may be releasing poisonous gas\u201d", "https://scroll.in/latest/942794/bjp-leader-blames-pakistan-and-china-for-pollution-says-they-may-be-releasing-poisonous-gases"],
  ["\u201cStubble burning in Punjab is solely responsible\u201d", "https://business-standard.com/article/pti-stories/stubble-burning-in-pb-solely-responsible-for-rising-delhi-pollution-kejriwal-118110100928_1.html"],
  ["Blame the Centre", "https://www.thequint.com/news/hot-news/delhi-slams-centre-neighbouring-states-over-stubble-burning"],
  ["\u201cThere's no doubt who turned Delhi into a gas chamber\u201d", "https://theprint.in/india/theres-no-doubt-who-turned-delhi-into-a-gas-chamber-union-minister-bhupender-yadav-attacks-aap/1193970"],
  ["\u201cSmoke from Punjab doesn't reach Delhi\u201d", "https://goemkarponn.com/smoke-from-punjab-does-not-reach-delhi-says-chief-minister-mann"],
  ["\u201cWhy is Delhi's air bad in December then?\u201d", "https://scroll.in/latest/942169/signs-of-poor-leadership-punjab-cm-hits-back-after-kejriwal-blames-pollution-on-stubble-burning"],
  ["\u201cPunjab is being defamed\u201d", "https://www.tribuneindia.com/news/amritsar/punjab-defamed-on-delhi-pollution-dhaliwal"],
  ["\u201cThe other party is forcing farmers to burn\u201d", "https://www.deccanchronicle.com/nation/current-affairs/delhi-govt-blames-punjab-aap-regime-for-toxic-haze-after-diwali-1911532"],
  ["\u201cAQI rose only 11 points after Diwali\u201d", "https://www.businesstoday.in/amp/india/story/delhi-pollution-aqi-rose-only-11-points-after-diwali-says-minister-manjinder-sirsa-499061-2025-10-21"],
  ["Blame the other party's crackers", "https://www.indiatvnews.com/news/india/delhi-air-pollution-smog-gopal-rai-bjp-crackers-diwali-aqi-latest-updates-743749"],
  ["\u201cWhen the wind stops, pollution settles\u201d", "https://www.newkerala.com/news/o/when-wind-stops-emissions-keep-happening-pollution-settles-bhupender-575"],
  ["\u201cFarmers aren't dodging satellites\u201d", "https://businesstoday.in/visualstories/news/indias-pollution-mystery-are-punjab-haryana-farmers-outsmarting-nasas-satellite-detection-190011-27-11-2024"],
  ["Smog tower inaugurated", "https://www.aljazeera.com/news/2021/8/23/india-capital-new-delhi-smog-tower-air-pollution"],
  ["Odd-even is back", "https://www.outlookindia.com/national/odd-even-scheme-in-delhi-to-combat-air-pollution-says-environment-minister-gopal-rai-news-329042"],
  ["Artificial rain, attempt #2", "https://www.theweek.in/news/india/2026/11/05/delhi-attempts-artificial-rain-again-amid-diwali-celebrations-why-it-failed-last-year.html"],
  ["Schools go hybrid, problem solved", "https://www.businesstoday.in/india/story/as-grap-iii-kicks-in-delhi-ncr-schools-directed-to-shift-classes-1-5-to-hybrid-mode-566315-2026-11-07"],
  ["\u201cThe bio-decomposer will fix it\u201d", "https://thewire.in/environment/pusa-decomposer-success-stubble-burning-arvind-kejriwal"],
  ["\u201cFines have been doubled!\u201d", "https://www.livelaw.in/top-stories/after-supreme-court-rap-centre-doubles-penalties-for-stubble-burning-farmers-to-pay-up-to-rs-30000-for-violations-274583"],
  ["\u201cAn expert committee has been formed\u201d", "https://www.deccanherald.com/amp/story/india%2Fdelhi%2Fcentre-aims-to-stamp-out-farm-fires-with-satellite-images-of-scorched-earth-3287678"],
  ["\u201cBurnt-area protocol under validation\u201d", "https://indianexpress.com/article/cities/delhi/stubble-burning-air-pollution-supreme-court-protocol-burnt-area-farm-fires-10959536/"],
  ["Straw-to-ethanol plant inaugurated", "https://renewable-carbon.eu/news/?p=115410"],
  ["FIRs and red entries against farmers", "https://www.tribuneindia.com/news/punjab/hc-seeks-punjabs-response-on-punitive-actions-against-farmers-over-stubble-burning"],
  ["Winter Action Plan\u2122 launched", "https://indianexpress.com/article/cities/delhi/delhi-winter-action-plan-november-1-construction-sites-inspected-10949485/"],
  ["\u20b9544 crore more for machines", "https://www.newkerala.com/news/a/over-rs-544-crore-allotted-crop-residue-management-478.htm"],
];
const BINGO_FREE = ["FREE: GRAP Stage III invoked", "https://www.theweek.in/news/india/2026/11/07/aqi-what-is-the-imd-forecast-for-delhi-as-grap-iii-is-imposed-as-a-diwali-precautionary-measure.html"];
const BINGO_WINS = [
  "BINGO! 🎉 Congratulations, you survived a press conference. Your prize: the same air as yesterday.",
  "BINGO! 🏆 Five excuses in a row. You are now qualified to chair a committee.",
  "BINGO! 🎊 Please collect your prize (one N95 mask) from the nearest smog tower.",
];

function newBingo() {
  const pool = [...EXCUSES];
  for (let i = pool.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [pool[i], pool[j]] = [pool[j], pool[i]]; }
  const cells = pool.slice(0, 24);
  cells.splice(12, 0, BINGO_FREE);
  const grid = $("bingo");
  grid.innerHTML = "";
  cells.forEach(([text, url], i) => {
    const free = i === 12;
    const cell = document.createElement("div");
    cell.className = "cell" + (free ? " free on" : "");
    const b = document.createElement("button");
    b.type = "button";
    b.className = "cell-btn";
    b.setAttribute("aria-pressed", free ? "true" : "false");
    b.textContent = text;
    if (free) b.disabled = true;
    else b.addEventListener("click", () => {
      const on = cell.classList.toggle("on");
      b.setAttribute("aria-pressed", String(on));
      checkBingo();
    });
    const a = document.createElement("a");
    a.className = "cell-src";
    a.href = url;
    a.target = "_blank";
    a.rel = "noopener";
    a.textContent = "\u2197";
    a.title = "Source: who actually said or did this";
    a.setAttribute("aria-label", `Source for: ${text}`);
    cell.append(b, a);
    grid.appendChild(cell);
  });
  $("bingo-msg").textContent = "\u00a0";
  $("bingo").classList.remove("won");
}

function checkBingo() {
  const on = [...$("bingo").children].map((c) => c.classList.contains("on"));
  const lines = [];
  for (let i = 0; i < 5; i++) {
    lines.push([0, 1, 2, 3, 4].map((j) => i * 5 + j));
    lines.push([0, 1, 2, 3, 4].map((j) => j * 5 + i));
  }
  lines.push([0, 6, 12, 18, 24], [4, 8, 12, 16, 20]);
  const won = lines.some((l) => l.every((i) => on[i]));
  const count = on.filter(Boolean).length - 1;
  $("bingo-msg").textContent = won
    ? BINGO_WINS[Math.floor(Math.random() * BINGO_WINS.length)]
    : count > 0 ? `${count} excuse${count > 1 ? "s" : ""} heard. Keep listening; they always come back.` : " ";
  $("bingo").classList.toggle("won", won);
}

async function main() {
  newBingo();
  renderGantt();
  initGlossary();
  $("bingo-new").addEventListener("click", newBingo);
  try {
    [state.manifest, state.districtsGeo] = await Promise.all([loadJSON("data/manifest.json"), loadJSON("data/districts.geojson")]);
  } catch (e) {
    $("freshness").textContent = "Could not load data: " + e.message;
    return;
  }
  const m = state.manifest;
  const gen = new Date(Date.parse(m.generated_at) + IST_OFFSET_MS).toISOString().slice(0, 16).replace("T", " ");
  const last = m.last_detection ? unixToIstInput(Date.parse(m.last_detection) / 1000).replace("T", " ") : "none yet";
  $("freshness").textContent = `Updated ${gen} IST · latest detection ${last} IST.`;
  chartDefaults();
  initStates();
  initFromHash();
  initMap();
  document.querySelectorAll("[data-preset]").forEach((b) => b.addEventListener("click", () => { applyPreset(b.dataset.preset); refresh(); }));
  $("filters").addEventListener("submit", (e) => { e.preventDefault(); refresh(); });
  renderGap();
  await Promise.all([refresh(), renderHero(), renderSeasons()]);
}


// ---- glossary: first use of each term gets a tap/hover definition ----
// [term (as it appears), definition, case-insensitive?]
const GLOSSARY = [
  ["parali", "Hindi/Punjabi for paddy stubble: the straw left in the field after the rice harvest.", true],
  ["mandi", "A government-regulated wholesale market where farmers sell their crops.", true],
  ["MSP", "Minimum Support Price: a price the national government announces each season for about two dozen crops. In practice it's only reliably paid for wheat and paddy, which the government buys in bulk."],
  ["kharif", "The monsoon crop season: sown June–July, harvested September–November (paddy, maize, cotton).", true],
  ["rabi", "The winter crop season: sown October–December, harvested March–May (wheat, mustard).", true],
  ["lakh", "100,000 (written 1,00,000 in India). 3 lakh = 300,000.", true],
  ["crore", "10 million (100 lakh). ₹4,266 crore = ₹42.66 billion.", true],
  ["quintal", "100 kilograms.", true],
  ["AQI", "Air Quality Index: India's 0–500 scale. 201–300 is \"poor\", 301–400 \"very poor\", 401 and above \"severe\"."],
  ["PM2.5", "Fine particles under 2.5 micrometres across, small enough to get deep into the lungs and bloodstream."],
  ["CPCB", "Central Pollution Control Board: the national pollution regulator, which publishes the official daily AQI."],
  ["NCR", "National Capital Region: Delhi plus surrounding districts of Haryana, Uttar Pradesh and Rajasthan."],
  ["GRAP", "Graded Response Action Plan: staged emergency curbs in Delhi-NCR (Stage I–IV), such as construction bans and school closures, triggered by AQI levels."],
  ["CAQM", "Commission for Air Quality Management: the national body set up in 2020 to coordinate air-pollution policy for Delhi-NCR and neighbouring states."],
  ["Union", "India's national (federal) government, also called \"the Centre\"."],
  ["the Centre", "India's national (federal) government, also called the Union government."],
  ["CM", "Chief Minister: the head of a state government."],
  ["MLA", "Member of the Legislative Assembly: an elected state legislator."],
  ["NGT", "National Green Tribunal: a specialised court for environmental cases."],
  ["IARI", "Indian Agricultural Research Institute, the national farm-research body in Delhi."],
  ["ISRO", "Indian Space Research Organisation, the national space agency."],
  ["NRSC", "National Remote Sensing Centre: ISRO's satellite-data centre."],
  ["RTI", "Right to Information: India's freedom-of-information law, used to request records from government bodies."],
  ["FIR", "First Information Report: the police complaint that starts a criminal case."],
  ["red entries", "Adverse marks in a farmer's land records for stubble burning, which can block loans and government benefits.", true],
  ["Happy Seeder", "A tractor-mounted machine that sows wheat straight into the paddy stubble, so the straw doesn't need to be burned.", true],
  ["bio-decomposer", "A microbial spray (the \"Pusa decomposer\", from IARI) meant to break down straw in the field over a few weeks.", true],
  ["FRP", "Fire Radiative Power: how much heat a fire is giving off, measured by the satellite in megawatts (MW)."],
  ["moong", "Mung bean, a pulse (lentil-like crop) often grown in the short gap between wheat and paddy.", true],
  ["kinnow", "A mandarin-orange hybrid widely grown in south-west Punjab.", true],
  ["Malwa", "The southern region of Punjab, the state's largest."],
  ["Diwali", "The Hindu festival of lights in October or November, celebrated with firecrackers."],
];

let tipEl = null;
function showTip(el) {
  if (!tipEl) {
    tipEl = document.createElement("div");
    tipEl.className = "tip";
    tipEl.setAttribute("role", "tooltip");
    tipEl.id = "term-tip";
    document.body.appendChild(tipEl);
  }
  tipEl.textContent = el.dataset.tip;
  tipEl.style.display = "block";
  const r = el.getBoundingClientRect();
  const w = tipEl.offsetWidth, h = tipEl.offsetHeight;
  const left = Math.min(Math.max(8, r.left + r.width / 2 - w / 2), window.innerWidth - w - 8);
  const top = r.top - h - 8 >= 8 ? r.top - h - 8 : r.bottom + 8;
  tipEl.style.left = `${left + window.scrollX}px`;
  tipEl.style.top = `${top + window.scrollY}px`;
  el.setAttribute("aria-describedby", "term-tip");
}
function hideTip(el) {
  if (tipEl) tipEl.style.display = "none";
  if (el) el.removeAttribute("aria-describedby");
}

function initGlossary() {
  const root = document.body;
  const skip = new Set(["A", "BUTTON", "SCRIPT", "STYLE", "LABEL", "SELECT", "OPTION", "INPUT", "CANVAS", "DT", "DD", "TH", "FOOTER", "TITLE"]);
  const esc = (t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  for (const [term, def, ci] of GLOSSARY) {
    const re = new RegExp(`(^|[^A-Za-z])(${esc(term)}s?)(?![A-Za-z])`, ci ? "i" : "");
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode(n) {
        for (let p = n.parentElement; p && p !== root; p = p.parentElement) {
          if (skip.has(p.tagName) || p.classList.contains("term") || p.classList.contains("tip") || p.id === "glossary" || p.closest("#bingo, .leaflet-container")) return NodeFilter.FILTER_REJECT;
        }
        return re.test(n.nodeValue) ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP;
      },
    });
    const node = walker.nextNode();
    if (!node) continue;
    const m = re.exec(node.nodeValue);
    const start = m.index + m[1].length;
    const after = node.splitText(start);
    after.splitText(m[2].length);
    const span = document.createElement("span");
    span.className = "term";
    span.tabIndex = 0;
    span.dataset.tip = def;
    span.textContent = after.nodeValue;
    after.replaceWith(span);
    span.addEventListener("mouseenter", () => showTip(span));
    span.addEventListener("mouseleave", () => hideTip(span));
    span.addEventListener("focus", () => showTip(span));
    span.addEventListener("blur", () => hideTip(span));
  }
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") hideTip(document.activeElement); });
  window.addEventListener("scroll", () => hideTip(), { passive: true });

  const seen = new Set();
  $("glossary-list").innerHTML = GLOSSARY
    .filter(([t, d]) => !seen.has(d) && seen.add(d))
    .sort((a, b) => a[0].localeCompare(b[0], "en", { sensitivity: "base" }))
    .map(([t, d]) => `<div><dt>${t}</dt><dd>${d}</dd></div>`).join("");
}

main();
