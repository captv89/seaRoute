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
