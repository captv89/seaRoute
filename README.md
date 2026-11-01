# seaRoute — Tactical Maritime ECDIS & Route Optimization Engine

[![golangci-lint](https://github.com/captv89/seaRoute/actions/workflows/lint.yml/badge.svg)](https://github.com/captv89/seaRoute/actions/workflows/lint.yml)
[![go-coverage-badge](https://img.shields.io/badge/Coverage-68.9%25-yellow)](https://github.com/captv89/seaRoute/actions/workflows/test.yml)
[![Docker Image](https://img.shields.io/badge/docker-ghcr.io%2Fcaptv89%2Fsearoute-blue?logo=docker)](https://github.com/captv89/seaRoute/pkgs/container/searoute)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**seaRoute** is a high-performance maritime navigation, ECDIS like visualizer, and optimal sea route computation engine written in Go and Leaflet. Built on top of the Eurostat Marnet navigation mesh (42,800+ nodes, 118,400+ nautical edges), it calculates shortest, navigable sea passages between any two global ports or arbitrary marine coordinates. Layers include maritime boundaries, piracy risk zones, emissions control areas, and live weather conditions.

---
## Screenshot
![](https://i.ibb.co/KcD1zv6G/sea-Route-v2.png)

![](https://i.ibb.co/pv4P78nc/Screenshot-2026-10-21-at-21-18-04.png)


## Key Features

### 1. Advanced Routing & Passage Optimization
* **Dual Routing Engines**: Choose between standard **Dijkstra** (guaranteed lowest cumulative nautical weight) and heuristic **A\*** (accelerated A-Star graph traversal).
* **Strategic Choke-Point Avoidance**: Restrict or permit transit through critical maritime passages, including **Suez Canal, Panama Canal, Strait of Malacca, Kiel Canal, Strait of Gibraltar, Bab-el-Mandeb, Dover Strait**, and Arctic routes (**Northeast / Northwest Passages**).
* **Flexible Coordinate Input**: Enter origin and destination by port name / UN/LOCODE (with instant autocomplete across 4,300+ ports), Decimal Degrees, Nautical Degrees-Minutes-Seconds (DMS), or click directly on the interactive map canvas.

### 2. Tactical ECDIS Interface
* **Tactical Design System**: Crisp, high-contrast maritime interface optimized for low-glare bridge use, featuring **Deep Sea Dark** and **Nautical Daylight** modes.
* **Transit Simulation & Vessel Telemetry**: Interactive passage player with scrubber track, real-time simulated coordinates, elapsed days, and variable speed multipliers (**1x, 5x, 20x**).
* **Dynamic Maritime Scale Bar**: Dynamic nautical distance scale dock at the bottom-left calibrated in **Nautical Miles (NM)** and **Kilometers (KM)** with live Mercator zoom projection indicator.
* **Manifest & Export**: Full tabular waypoint manifest breakdown with individual leg bearings, nautical distances, and single-click **GeoJSON** or **CSV** export.

### 3. Maritime GIS & Tactical Overlays
* **Territorial Waters (12 NM Baseline)**: High-resolution territorial sea limits rendered via client vector tiles.
* **Exclusive Economic Zones (EEZ & Boundaries)**: 200 NM sovereign economic boundaries and median lines.
* **Piracy & Maritime Security (ASAM / HRA)**: High Risk Areas (HRA) polygon and live feed of verified piracy and armed robbery incidents with interactive tactical debrief cards.
* **World Seaport Database (WPI)**: 3,600+ commercial harbors with harbor size, water depth, entrance width, and vessel draft constraints.
* **IMO Emission Control Areas (ECA / SECA)**: SOx 0.10% and NOx Tier III compliance boundary visualization.
* **FAO Fishing Zones**: Major marine fishing areas for regulatory monitoring.

### 4. Dynamic Weather Intensity Matrix & Scale
* **Composable Weather Matrix**: Overlay 8 meteorological layers powered by OpenWeatherMap 2.0: **Wind Speed, Wave Height, Precipitation, Weather Radar, Sea/Air Temperature, Atmospheric Pressure, Cloud Coverage**, and **Snowfall**.
* **Calibrated Intensity Scale Dock**: Positioned alongside the maritime distance scale dock, rendering continuous meteorological color gradients with calibrated physical unit ticks (**KTS, M, MM/H, °C, hPa, %**).
* **Multi-Layer Selector**: Clickable pill tabs automatically appear when multiple weather layers are enabled simultaneously, allowing swift legend toggling without interrupting map layers.

### 5. Model Context Protocol (MCP) Integration
* Built-in native **MCP Server** (`/mcp`) enabling AI agents and assistants (Antigravity, Claude, ChatGPT) to programmatically compute sea routes, inspect waypoints, query seaport coordinates, and analyze passage constraints.

---

## Quick Start with Docker

The official container image is pre-built, self-contained, and hosted on the GitHub Container Registry. It bundles all pre-compressed, optimized datasets internally without requiring local volume mounts.

### Using Docker Compose (Recommended)

1. Create a `docker-compose.yml`:

```yaml
services:
  searoute:
    image: ghcr.io/captv89/searoute:latest
    ports:
      - "8080:8080"
    env_file:
      - .env
    restart: unless-stopped
```

2. Create a `.env` file for configuration:

```env
PORT=8080
APP_ENV=production
# Optional: CARTO basemaps API key (falls back to OpenStreetMap if omitted)
CARTO_API_KEY=
# Optional: OpenWeatherMap API key for weather layers
OWM_API_KEY=
# Optional: Google Analytics measurement ID
GA_TRACKING_ID=
```

3. Launch the container:

```bash
docker compose up -d
```

Access the application in your browser at `http://localhost:8080`.

### Using Docker CLI Directly

```bash
docker run -d \
  -p 8080:8080 \
  --name searoute \
  --env-file .env \
  ghcr.io/captv89/searoute:latest
```

---

## Building from Source

### Prerequisites
* [Go 1.22+](https://go.dev/)
* GCC / CGO build toolchain (for SQLite embedded driver)

### Installation

```bash
# Clone repository
git clone https://github.com/captv89/seaRoute.git
cd seaRoute

# Run tests
go test -v ./...

# Run the local server
go run .
```

Open `http://localhost:8080` in your web browser.

---

## Configuration & Environment Variables

| Variable | Description | Default |
|---|---|---|
| `PORT` | HTTP server listening port | `8080` |
| `APP_ENV` | Application environment (`development` / `production`) | `production` |
| `OWM_API_KEY` | OpenWeatherMap API key for live radar and weather matrix | _Optional_ |
| `CARTO_API_KEY` | CARTO vector basemap key (uses OpenStreetMap if omitted) | _Optional_ |
| `GA_TRACKING_ID`| Google Analytics tracking identifier | _Optional_ |
| `CSP_MODE` | `report-only` sends the Content-Security-Policy as `Content-Security-Policy-Report-Only` (fallback if the enforced policy blocks something) | enforced |
| `TRUSTED_PROXIES` | Comma-separated proxy CIDRs allowed to set `X-Forwarded-For` (used for per-client rate limiting); `none` to ignore the header | loopback + private ranges |

`/waypoints`, `/ports` and `/mcp` are rate limited to 60 requests per minute per client IP. Pages, static files and map layers are not.

### Rebuilding the CSS

The UI uses Tailwind CSS v3, prebuilt into `web/static/tailwind.css` (committed, so Go and Docker builds need no Node). After changing classes in the templates or `web/static/script.js`, rebuild it:

```bash
cd web/tailwind && npm ci && npm run build
```

---

## API Endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/` | `GET` | Main Tactical ECDIS dashboard web application |
| `/waypoints` | `POST` | Calculate optimal sea route between ports or coordinates |
| `/ports` | `GET` | Query commercial seaport database with UN/LOCODEs |
| `/passages` | `GET` | List available choke-point passages and routing graph stats |
| `/network` | `GET` | Stream raw Marnet navigation graph GeoJSON (gzip when accepted) |
| `/layers/:name` | `GET` | Serve gzip-compressed tactical GIS overlays (`baseline`, `eez`, `piracy`, `seaports`, `eca-sox`, `eca-nox`, `boundaries`, `fao`, `tss`) |
| `/emissions` | `GET` | Stream IMO Emission Control Area (ECA) GeoJSON |
| `/mcp` | `GET/POST` | Model Context Protocol endpoint for AI agent integration |

### `POST /waypoints` fields

Request (optional field):

| Field | Default | Description |
|---|---|---|
| `clearance_nm` | `2` | Land clearance hazards are measured against, 0.5 to 10 NM. Anything outside the range returns HTTP 400. |
| `draught_m` | `10` | Vessel draught in metres, 1 to 25. An explicit `0` or out-of-range value returns HTTP 400. |
| `speed_kn` | `14` | Speed in knots for squat, 1 to 30. An explicit `0` or out-of-range value returns HTTP 400. |
| `vessel_type` | `bulk` | `tanker`, `bulk`, `general` or `container`; sets the block coefficient (0.80, 0.80, 0.70, 0.60). |

The route stays in water deeper than draught + squat + margin where depth data exists. Squat is Cb x V^2 / 100 in open water and Cb x V^2 / 50 in canals and straits; the margin is the larger of 2 m and 10% of the draught. When the draught does not fit, the route goes the long way and `notices` say why. The same fields (`draught_m`, `speed_kn`, `vessel_type`) are accepted by the MCP `route` and `distance_to_go` tools.

The `kinks` and `accept` request fields were removed and are now ignored; `proposals` and `ignored_accept` are no longer returned.

Traffic separation scheme (TSS) handling is always on. Every leg is a rhumb line.

Response `properties` gains `clearance_nm` (the effective value), `stretches[]` (index range into the path, `flag`, `severity`, `scheme`), `warnings[]`, `hazards[]`, `depth` (`draught_m`, `squat_m`, `margin_m` and `depth_limit_m`, the depth needed in open water), `confined_depth_limit_m` (the limit in a canal or strait) and `notices[]` (empty when the draught changed nothing; each has `kind` `draught-reroute` or `draught-no-route`, `text`, `extra_nm`, `avoided` and `passages`). The MCP tools return `depth`, `confined_depth_limit_m` and `notices` too. Each hazard is a run of path segments that touch land (`severity` `aground`, within the land data tolerance) or pass closer than `clearance_nm` (`close`), or a port connector that leaves the area the land data covers (`unchecked`, clearance unknown), with `from`/`to` (index range into the path), `start`/`end` positions, `min_clearance_nm`, `kind` (`marnet`, `osm-tss`, `connector`), `passage` and `note`. The MCP `route` and `distance_to_go` tools return the same `hazards`. Each waypoint has a `flag` naming the provenance of the stretch that arrives at it (`marnet`, `osm-tss`, `tss-unknown`, `tss-unresolved`, `connector`); the origin is `connector`.

---

## Limitations

* TSS data is community-maintained (OpenSeaMap / OpenStreetMap) and its coverage is incomplete. Stretches near land outside the covered areas are flagged `tss-unknown`; where TSS rules leave no route, the flagged `tss-unresolved` stretch ignores one scheme.
* The land data is simplified (50 m) and covers a corridor around the MARNET network. Canals and channels narrower than that read as aground and are flagged as hazards. MARNET itself is not a surveyed track.
* A licensed ENC (S-57/S-101 from a hydrographic office or chart distributor) is the authoritative source for charted hazards and traffic schemes. seaRoute does not use one.
* Depths come from GEBCO 2026 and land from OpenStreetMap. Both are approximate and not for navigation; check the official chart.
* Vessel inputs: `draught_m` (default 10), `speed_kn` (default 14) and `vessel_type` (`tanker`, `bulk`, `general` or `container`; default `bulk`). The route needs water at least as deep as `draught + squat + margin`. Squat follows Barrass, `Cb x V^2 / 100` metres in open water and `Cb x V^2 / 50` in a canal or strait (block coefficient Cb: tanker 0.80, bulk 0.80, general 0.70, container 0.60; V in knots). The margin for depth data error is `max(2 m, 0.10 x draught)`. Passages with a draught limit (Kiel, Suez, Panama and others) close when the vessel needs more. Near each port, the first stretch up to 15 NM is exempt until the track first reaches safe water.
* The Kiel Canal limit (9.5 m) applies to ships up to about 160 m length. Longer ships face lower limits, which are not modelled here.
* seaRoute is not for navigation. A passage plan still needs official charts.

---

## References & Data Sources

* **Marnet Routing Mesh**: Derived from the Eurostat [searoute](https://github.com/eurostat/searoute) project.
* **Maritime Boundaries & EEZ**: [Marine Regions](https://www.marineregions.org/) (Flanders Marine Institute).
* **Anti-Shipping Activity Messages (ASAM)**: National Geospatial-Intelligence Agency ([NGA](https://msi.nga.mil/Piracy)).
* **World Port Index (WPI)**: National Geospatial-Intelligence Agency ([NGA Pub 150](https://msi.nga.mil/Publications/WPI)).
* **Land and Traffic Separation Schemes**: © OpenStreetMap contributors, ODbL ([openstreetmap.org/copyright](https://www.openstreetmap.org/copyright)). Land from OSM land polygons, TSS from OpenSeaMap seamark tags. Rebuild with:
  `go run ./cmd/land-import -shp land_polygons.shp -network dataset/marnet_plus_5km.geojson -tolerance 200 -out dataset/land_osm.bin.gz` and
  `go run ./cmd/tss-import -in overpass.json -out dataset/tss_osm.geojson.gz`.
* **Bathymetry**: GEBCO Bathymetric Compilation Group (2026) GEBCO 2026 Grid (doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc036fa). Rebuild with `go run ./cmd/depth-import` (see `dataset/README.md`).
* **Emission Control Areas**: International Maritime Organization ([IMO MARPOL Annex VI](https://www.imo.org/)).

---

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
