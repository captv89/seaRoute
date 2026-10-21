# seaRoute — Tactical Maritime ECDIS & Route Optimization Engine

[![Go Report Card](https://goreportcard.com/badge/github.com/captv89/seaRoute)](https://goreportcard.com/report/github.com/captv89/seaRoute)
[![Docker Image](https://img.shields.io/badge/docker-ghcr.io%2Fcaptv89%2Fsearoute-blue?logo=docker)](https://github.com/captv89/seaRoute/pkgs/container/searoute)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**seaRoute** is a high-performance maritime navigation, ECDIS visualizer, and optimal sea route computation engine written in Go and Leaflet. Built on top of the Eurostat Marnet navigation mesh (42,800+ nodes, 118,400+ nautical edges), it calculates fuel-efficient, navigable sea passages between any two global ports or arbitrary marine coordinates, accounting for maritime boundaries, piracy risk zones, emissions control areas, and live weather conditions.

---

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

---

## API Endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/` | `GET` | Main Tactical ECDIS dashboard web application |
| `/waypoints` | `POST` | Calculate optimal sea route between ports or coordinates |
| `/ports` | `GET` | Query commercial seaport database with UN/LOCODEs |
| `/passages` | `GET` | List available choke-point passages and routing graph stats |
| `/network` | `GET` | Stream raw Marnet navigation graph GeoJSON |
| `/layers/:name` | `GET` | Serve gzip-compressed tactical GIS overlays (`baseline`, `eez`, `piracy`, `seaports`, `eca-sox`, `eca-nox`, `boundaries`, `fao`) |
| `/emissions` | `GET` | Stream IMO Emission Control Area (ECA) GeoJSON |
| `/mcp` | `GET/POST` | Model Context Protocol endpoint for AI agent integration |

---

## References & Data Sources

* **Marnet Routing Mesh**: Derived from the Eurostat [searoute](https://github.com/eurostat/searoute) project.
* **Maritime Boundaries & EEZ**: [Marine Regions](https://www.marineregions.org/) (Flanders Marine Institute).
* **Anti-Shipping Activity Messages (ASAM)**: National Geospatial-Intelligence Agency ([NGA](https://msi.nga.mil/Piracy)).
* **World Port Index (WPI)**: National Geospatial-Intelligence Agency ([NGA Pub 150](https://msi.nga.mil/Publications/WPI)).
* **Emission Control Areas**: International Maritime Organization ([IMO MARPOL Annex VI](https://www.imo.org/)).

---

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
