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
- Source: OSM land polygons, split, EPSG:4326 (https://osmdata.openstreetmap.de/download/land-polygons-split-4326.zip), downloaded 2026-11-01; header date 2026-10-31 is the date the files inside carry
- Licence: © OpenStreetMap contributors, ODbL
- Only rings whose bounding box touches a 0.5° cell within 30 NM of the MARNET network are kept (224,142 of 873,918 rings). The corridor follows every edge, densified to points at most 5 NM apart (`internal/corridor`), not only the vertices: long MARNET edges cut across islands and peninsulas, and the router must see that land. Simplified with Douglas–Peucker at 50 m, latitude-aware and conservative so every removed vertex is within 50 m (4,884,921 vertices kept of 79,494,513)
- Format: gzip; line `SRLAND1`, one JSON header line (`source`, `date`, `tolerance_m`, `corridor_nm`, `rings`), then per ring a uvarint vertex count and zigzag-varint (dlon, dlat) deltas in 1e-6°, first vertex relative to (0,0)
- Size: 19,803,009 bytes (19.8 MB); 16.4 MB with the old vertex-only corridor. Tolerance 100 m gives 9.1 MB, 200 m gives 4.9 MB. Measured with `route.LoadLand` (Apple M-series dev machine): load 0.6 s, live heap 54 MB after load (limits: 5 s, 300 MB)
- Regenerate: `go run ./cmd/land-import -shp <land_polygons.shp> -network dataset/marnet_plus_5km.geojson -tolerance 50 -date 2026-10-31 -out dataset/land_osm.bin.gz`

## tss_osm.geojson.gz — traffic separation schemes
- Source: OpenStreetMap ways tagged `seamark:type=separation_*`, fetched from the Overpass API (https://overpass-api.de/api/interpreter) on 2026-10-23, `osm_base` 2026-10-23T12:01:23Z (fresh fetch, second attempt; the first got HTTP 502). The recon copy was not used.
- Licence: © OpenStreetMap contributors, ODbL
- Query (exact): `[out:json][timeout:300];way["seamark:type"~"^separation_(lane|zone|line|boundary|roundabout|crossing)$"];out tags geom;`
- Import: 2,354 ways; 156 skipped because they carry a `waterway` tag (inland canals mis-tagged as TSS, e.g. Amsterdam's Prinsengracht); 0 unknown types. Output 2,198 features in 210 schemes: lane 798, zone 364, boundary 878, line 150, roundabout 5, crossing 3.
- Schemes: ways whose bounding boxes lie within 3 NM are grouped (union-find). Scheme ID `tss-` + first 8 hex of sha256 of the sorted OSM way IDs joined by `,`. Name is the first `seamark:name`/`name` tag, else the ID.
- Format: gzip GeoJSON FeatureCollection with top-level `osm_base`. Feature properties `type` (seamark type without `separation_`), `osm_id`, `scheme`, `name`. Geometry is a LineString, or a Polygon for closed zone, roundabout and lane ways. Coordinates rounded to 1e-6°. Features are sorted by `osm_id`, so output is deterministic.
- Size: 100,788 bytes. Loaded by `route.LoadTSS`, which builds each scheme's 1 NM area and 20 NM coverage hulls.
- Regenerate: `go run ./cmd/tss-import -in <overpass.json> -out dataset/tss_osm.geojson.gz`

## depth_gebco.bin.gz — water depth for draught limits
- Source: GEBCO Bathymetric Compilation Group (2026) GEBCO 2026 Grid (doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc036fa), ice-surface elevation, Esri ASCII tiles (8 tiles, 15 arc-seconds, about 20 GB). The raw grid is not committed.
- Licence: public domain; attribution requested (the credit line above).
- Format: gzip of `SRDEPTH2\n` + JSON header + per-tile cells + the worldwide deep-ocean mask. Cell values: 0 = land, drying or NODATA, 1-254 = depth in whole metres rounded down, 255 = 255 m or deeper. Tiles are 0.5 degrees (120x120 cells); an all-255 tile is stored as `Deep` with no cells. The mask is 720x360 bytes (one per 0.5 degree tile worldwide, row-major from 180 W / 90 S) holding each tile's minimum depth in the same encoding (0 for tiles with no data). `SRDEPTH1` files (no mask) still load. Read by `route.LoadDepth`.
- Coverage: tiles within 30 NM of the MARNET network, every edge densified (the same corridor as the land data): 113,136 tiles, of which 88,788 are Deep and 24,348 hold cells. The mask covers the whole globe.
- Size: 75,312,878 bytes (75.3 MB); the mask adds about 0.26 MB uncompressed.
- Load: about 0.95 s and 279 MB of heap for `LoadDepth` on the dev machine (Apple silicon).
- Build: 123 s and about 370 MB peak RSS (rows are streamed; no tile is held in memory).
- Regenerate: `go run ./cmd/depth-import -asc '/Volumes/Mac Drive/data/gebco_2026/gebco_2026_*_ascii.asc' -network dataset/marnet_plus_5km.geojson -corridor 30 -date 2026-01-01 -out dataset/depth_gebco.bin.gz`
