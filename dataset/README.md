# Datasets

## marnet_plus_5km.geojson — maritime network

- Source: Eurostat SeaRoute, `modules/core/src/main/resources/marnet/marnet_plus_5km.gpkg`
  at commit `0d777c0` (2021-09-08), https://github.com/eurostat/searoute
- Licence: EUPL-1.2 (European Union Public Licence). © European Union, Eurostat.
  The network itself is based on the Oak Ridge National Laboratory CTA Global Shipping
  Lane Network (2000), with additional AIS-derived lines around Europe.
- Converted with `go run ./cmd/marnet-import -in marnet_plus_5km.gpkg -out dataset/marnet_plus_5km.geojson`.
  Geometry and the `pass` tags are unchanged; coordinates are rounded to 6 decimals.
- Passage tags from Eurostat: suez, panama, malacca, gibraltar, dover, bering, magellan,
  babelmandeb, kiel, corinth, northwest, northeast. seaRoute adds `oresund` and
  `belle_isle` at load time (see `route.ExtraPassages`). The Arctic passages are closed
  unless a request opens them.

## port-list.sqlite — port names and positions

- Built by hand by the seaRoute author from UNECE UN/LOCODE (United Nations Code
  for Trade and Transport Locations), https://unece.org/trade/cefact/unlocode-code-list-country-and-territory
- UNECE publishes UN/LOCODE free of charge. No formal licence text was found;
  UNECE's website terms of use apply. Credit: "Source: UN/LOCODE, UNECE".

## ASAM_events.geojson — Piracy & Anti-Shipping Incidents
- Source: National Geospatial-Intelligence Agency (NGA) Anti-Shipping Activity Messages (ASAM)
- Licence: U.S. Government Work / Public Domain
- Pre-compressed with gzip for streaming over `GET /layers/piracy`

## WPI.geojson — World Port Index
- Source: National Geospatial-Intelligence Agency (NGA) Pub 150 World Port Index (3,600+ major maritime ports)
- Licence: U.S. Government Work / Public Domain
- Pre-compressed with gzip for streaming over `GET /layers/seaports`

## eez_v11 & eez_12nm_v3 — Exclusive Economic Zones & Territorial Sea Baseline
- Source: Flanders Marine Institute (VLIZ) Marine Regions (https://marineregions.org)
- Licence: CC-BY 4.0
- Optimized with coordinate quantization and pre-compressed with gzip (`GET /layers/eez`, `GET /layers/baseline`)

## eca_reg13_nox & eca_reg14_sox_pm — IMO Emission Control Areas
- Source: International Maritime Organization (IMO) MARPOL Annex VI Reg 13 (NOx Tier III) & Reg 14 (SOx 0.10% fuel sulfur)
- Optimized and pre-compressed with gzip for streaming over `GET /layers/eca-nox` and `GET /layers/eca-sox`

## FAO_AREAS_CWP.geojson — FAO Major Fishing Areas
- Source: Food and Agriculture Organization of the United Nations (FAO / CWP)
- Pre-compressed with gzip for streaming over `GET /layers/fao`

## land_osm.bin.gz — coastline for land-clearance checks
- Source: OSM land polygons, split, EPSG:4326 (https://osmdata.openstreetmap.de/download/land-polygons-split-4326.zip), downloaded 2026-10-23; header date 2026-10-23 is the zip's Last-Modified date (the files inside carry the same date)
- Licence: © OpenStreetMap contributors, ODbL
- Only rings whose bounding box touches a 0.5° cell within 30 NM of a MARNET vertex are kept (181,622 of 873,802 rings), simplified with Douglas–Peucker at 50 m, latitude-aware and conservative so every removed vertex is within 50 m (4,039,269 vertices kept of 79,479,292)
- Format: gzip; line `SRLAND1`, one JSON header line (`source`, `date`, `tolerance_m`, `corridor_nm`, `rings`), then per ring a uvarint vertex count and zigzag-varint (dlon, dlat) deltas in 1e-6°, first vertex relative to (0,0)
- Size: 16,357,187 bytes (16.4 MB). Tolerance 100 m gives 9.1 MB, 200 m gives 4.9 MB. Load time and heap are measured with the loader
- Regenerate: `go run ./cmd/land-import -shp <land_polygons.shp> -network dataset/marnet_plus_5km.geojson -tolerance 50 -date 2026-10-23 -out dataset/land_osm.bin.gz`
