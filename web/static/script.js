/**
 * seaRoute - Tactical Maritime Route Visualizer
 * Core Application Engine & Tactical ECDIS Logic
 */
(function () {
    'use strict';

    // =========================================================================
    // 1. App Configuration & API Keys
    // =========================================================================
    const OWM_API_KEY = (window.__APP_CONFIG__ && window.__APP_CONFIG__.owmApiKey) || '';
    const CARTO_API_KEY = (window.__APP_CONFIG__ && window.__APP_CONFIG__.cartoApiKey) || '';

    // =========================================================================
    // 2. Global State Variables
    // =========================================================================
    let currentAlgorithm = 'dijkstra';
    let currentRouteData = null;
    let currentTotalDistNM = 8420;
    let currentWaypoints = [];
    let currentCoords = [];
    let mapPickingMode = null; // null | 'origin' | 'dest'
    let currentGlowPolyline = null;
    let currentFlowPolyline = null;

    // Simulation state
    let simActive = false;
    let simPlaying = false;
    let simProgress = 0; // 0 to 100
    let simSpeedMultiplier = 1;
    let simInterval = null;
    let simVesselMarker = null;

    // Tactical Layer instances & states
    let marnetLayer = null;
    let marnetLoading = false;
    let hraLayer = null;
    let piracyLayer = null;
    let piracyLoading = false;
    let seaportsLayer = null;
    let seaportsLoading = false;
    let secaLayer = null;
    let secaLoading = false;
    let secaData = null;
    let ecaNoxLayer = null;
    let ecaNoxLoading = false;
    let ecaNoxData = null;
    let baselineLayer = null;
    let baselineLoading = false;
    let baselineData = null;
    let eezLayer = null;
    let eezLoading = false;
    let eezData = null;
    let faoLayer = null;
    let faoLoading = false;
    let faoData = null;

    // Toast timer
    let toastTimeout = null;

    // =========================================================================
    // 3. Coordinate Parsing & Formatting Utilities
    // =========================================================================

    /**
     * Parses Decimal or DMS coordinate string into a float.
     * Supports: "51.95", "-74.04", "51° 57.00' N (51.9500)", "40° 40.80' N", etc.
     */
    function parseCoordinate(input, isLat) {
        if (typeof input === 'number') return input;
        if (!input || typeof input !== 'string') return NaN;
        const str = input.trim();
        if (!str) return NaN;

        // Check if decimal inside parentheses e.g. "51° 57.00' N (51.9500)"
        const parenMatch = str.match(/\(([+-]?\d+(?:\.\d+)?)\)/);
        if (parenMatch) {
            const val = parseFloat(parenMatch[1]);
            if (!isNaN(val)) return val;
        }

        // Try pure float e.g. "51.95" or "-74.04"
        if (/^[+-]?\d+(?:\.\d+)?$/.test(str)) {
            return parseFloat(str);
        }

        // Check for DMS notation e.g. 51° 57.00' N or 74° 04.00' W
        const dmsMatch = str.match(/([+-]?\d+(?:\.\d+)?)\s*°?\s*(?:(\d+(?:\.\d+)?)\s*'?)?\s*(?:(\d+(?:\.\d+)?)\s*"?)?\s*([NSEWnsew])?/);
        if (dmsMatch) {
            const deg = parseFloat(dmsMatch[1]) || 0;
            const min = parseFloat(dmsMatch[2]) || 0;
            const sec = parseFloat(dmsMatch[3]) || 0;
            const dir = (dmsMatch[4] || '').toUpperCase();

            let decimal = Math.abs(deg) + (min / 60) + (sec / 3600);
            if (deg < 0 || dir === 'S' || dir === 'W') {
                decimal = -decimal;
            }
            return decimal;
        }

        const fallback = parseFloat(str);
        return isNaN(fallback) ? NaN : fallback;
    }

    /**
     * Formats decimal degrees into standard nautical DMS and decimal string.
     * Example: 51.95 -> "51° 57.00' N (51.9500)"
     */
    function formatCoordDMS(val, isLat) {
        if (val === undefined || val === null || isNaN(val)) return '--';
        const abs = Math.abs(val);
        const deg = Math.floor(abs);
        const min = ((abs - deg) * 60).toFixed(2);
        const dir = isLat ? (val >= 0 ? 'N' : 'S') : (val >= 0 ? 'E' : 'W');
        return `${String(deg).padStart(isLat ? 2 : 3, '0')}° ${min}' ${dir} (${val.toFixed(4)})`;
    }

    /**
     * Toast notification display helper
     */
    function showToast(message, iconName = 'check_circle') {
        const toast = document.getElementById('toast');
        const toastMsg = document.getElementById('toast-msg');
        const toastIcon = document.getElementById('toast-icon');
        if (!toast || !toastMsg) return;

        toastMsg.textContent = message;
        if (toastIcon && iconName) toastIcon.textContent = iconName;

        toast.classList.remove('translate-y-12', 'opacity-0');
        toast.classList.add('translate-y-0', 'opacity-100');

        if (toastTimeout) clearTimeout(toastTimeout);
        toastTimeout = setTimeout(() => {
            toast.classList.add('translate-y-12', 'opacity-0');
            toast.classList.remove('translate-y-0', 'opacity-100');
        }, 2800);
    }

    // =========================================================================
    // 4. Map Setup & Base Tile Layers
    // =========================================================================
    const map = L.map('map', {
        preferCanvas: true,
        zoomControl: false,
        attributionControl: true
    }).setView([20, 15], 3);

    // Reposition attribution control to bottom-right neatly
    map.attributionControl.setPosition('bottomright');

    // Base Layers definition
    const cartoDarkUrl = CARTO_API_KEY
        ? `https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png?key=${CARTO_API_KEY}`
        : 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png';

    const cartoLightUrl = CARTO_API_KEY
        ? `https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png?key=${CARTO_API_KEY}`
        : 'https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png';

    const baseLayers = {
        'carto-dark': L.tileLayer(cartoDarkUrl, {
            attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
            maxZoom: 18,
            subdomains: 'abcd'
        }),
        'carto-light': L.tileLayer(cartoLightUrl, {
            attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
            maxZoom: 18,
            subdomains: 'abcd'
        }),
        'osm': L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
            maxZoom: 19
        }),
        'satellite': L.tileLayer('https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}', {
            attribution: 'Tiles &copy; Esri &mdash; Source: Esri, i-cubed, USDA, USGS, AEX, GeoEye, Getmapping, Aerogrid, IGN, IGP, UPR-EGP, and the GIS User Community',
            maxZoom: 18
        })
    };

    // Default base layer: Carto Dark
    let currentBaseLayerKey = 'carto-dark';
    baseLayers[currentBaseLayerKey].addTo(map);

    function switchBaseLayer(key) {
        if (!baseLayers[key]) return;
        if (currentBaseLayerKey && baseLayers[currentBaseLayerKey]) {
            map.removeLayer(baseLayers[currentBaseLayerKey]);
        }
        currentBaseLayerKey = key;
        baseLayers[currentBaseLayerKey].addTo(map);
        baseLayers[currentBaseLayerKey].bringToBack();

        // Update button visual state
        document.querySelectorAll('.base-layer-btn').forEach(btn => {
            const isTarget = btn.getAttribute('data-layer') === key;
            if (isTarget) {
                btn.className = 'base-layer-btn active flex items-center justify-center gap-1 py-1 px-2 rounded-sm bg-primary/20 border border-primary text-primary text-[10px] font-semibold cursor-pointer';
            } else {
                btn.className = 'base-layer-btn flex items-center justify-center gap-1 py-1 px-2 rounded-sm bg-surface-container hover:bg-surface-container-high border border-surface-container-high text-on-surface-variant text-[10px] cursor-pointer';
            }
        });
    }

    // Connect base layer buttons
    document.querySelectorAll('.base-layer-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const layerKey = btn.getAttribute('data-layer');
            switchBaseLayer(layerKey);
            showToast(`Base chart: ${btn.textContent.trim()}`);
        });
    });

    // Zoom & Fit Bounds controls
    const btnZoomIn = document.getElementById('btn-zoom-in');
    const btnZoomOut = document.getElementById('btn-zoom-out');
    const btnFitBounds = document.getElementById('btn-fit-bounds');

    if (btnZoomIn) btnZoomIn.addEventListener('click', () => map.zoomIn());
    if (btnZoomOut) btnZoomOut.addEventListener('click', () => map.zoomOut());
    if (btnFitBounds) btnFitBounds.addEventListener('click', fitRouteBounds);

    function fitRouteBounds() {
        if (routeLayerGroup && routeLayerGroup.getLayers().length > 0) {
            const bounds = routeLayerGroup.getBounds();
            if (bounds.isValid()) {
                map.fitBounds(bounds, { padding: [50, 50] });
                return;
            }
        }
        map.setView([20, 15], 3);
    }

    // Dedicated layer group for calculated route graphics
    const routeLayerGroup = L.featureGroup().addTo(map);

    // Weather sub-layer instances & active state (Option A: Composable)
    const weatherSublayers = {};
    const activeWeatherLayers = new Set(['wind']);

    if (OWM_API_KEY) {
        weatherSublayers['clouds'] = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/CL/{z}/{x}/{y}?appid=${OWM_API_KEY}`, { maxZoom: 18, opacity: 0.6 });
        weatherSublayers['precip'] = L.tileLayer(`https://tile.openweathermap.org/map/precipitation_new/{z}/{x}/{y}?appid=${OWM_API_KEY}`, { maxZoom: 18, opacity: 0.75 });
        weatherSublayers['wind'] = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/WND/{z}/{x}/{y}?use_norm=false&opacity=0.85&arrow_step=16&appid=${OWM_API_KEY}`, { maxZoom: 18 });
        weatherSublayers['pressure'] = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/APM/{z}/{x}/{y}?appid=${OWM_API_KEY}`, { maxZoom: 18, opacity: 0.65 });
        weatherSublayers['temp'] = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/TA2/{z}/{x}/{y}?appid=${OWM_API_KEY}`, { maxZoom: 18, opacity: 0.55 });
        weatherSublayers['radar'] = L.tileLayer(`https://tile.openweathermap.org/map/precipitation_new/{z}/{x}/{y}?appid=${OWM_API_KEY}`, { maxZoom: 18, opacity: 0.9 });
        weatherSublayers['waves'] = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/WND/{z}/{x}/{y}?use_norm=true&opacity=0.7&appid=${OWM_API_KEY}`, { maxZoom: 18 });
        weatherSublayers['snow'] = L.tileLayer(`https://tile.openweathermap.org/map/snow/{z}/{x}/{y}.png?appid=${OWM_API_KEY}`, { maxZoom: 18, opacity: 0.6 });
    }

    // =========================================================================
    // 5. Tactical Overlays & Composable Weather Matrix Controls
    // =========================================================================
    const toggleMarnet = document.getElementById('toggle-marnet');
    const toggleOwm = document.getElementById('toggle-owm');
    const toggleHra = document.getElementById('toggle-hra');
    const togglePiracy = document.getElementById('toggle-piracy');
    const toggleSeaports = document.getElementById('toggle-seaports');
    const toggleSeca = document.getElementById('toggle-seca');
    const toggleEcaNox = document.getElementById('toggle-eca-nox');
    const toggleBaseline = document.getElementById('toggle-baseline');
    const toggleEez = document.getElementById('toggle-eez');
    const toggleFao = document.getElementById('toggle-fao');
    const layerActiveCount = document.getElementById('layer-active-count');

    function updateActiveLayerBadge() {
        if (!layerActiveCount) return;
        let count = 0;
        if (currentBaseLayerKey) count++;
        if (toggleOwm && toggleOwm.checked && activeWeatherLayers.size > 0) count++;
        if (toggleMarnet && toggleMarnet.checked) count++;
        if (toggleHra && toggleHra.checked) count++;
        if (togglePiracy && togglePiracy.checked) count++;
        if (toggleSeaports && toggleSeaports.checked) count++;
        if (toggleSeca && toggleSeca.checked) count++;
        if (toggleEcaNox && toggleEcaNox.checked) count++;
        if (toggleBaseline && toggleBaseline.checked) count++;
        if (toggleEez && toggleEez.checked) count++;
        if (toggleFao && toggleFao.checked) count++;
        layerActiveCount.textContent = count;
    }

    // =========================================================================
    // 5.0 Spatial Geometry & Bounding Box Utilities
    // =========================================================================
    function isPointInRing(point, ring) {
        if (!ring || ring.length < 3) return false;
        let inside = false;
        const x = point[0], y = point[1];
        for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
            const xi = ring[i][0], yi = ring[i][1];
            const xj = ring[j][0], yj = ring[j][1];
            const intersect = ((yi > y) !== (yj > y)) && (x < (xj - xi) * (y - yi) / (yj - yi) + xi);
            if (intersect) inside = !inside;
        }
        return inside;
    }

    function prepareGeoJsonBBoxes(geojson) {
        if (!geojson || !geojson.features) return;
        for (const feat of geojson.features) {
            if (feat.bbox && Array.isArray(feat.bbox) && feat.bbox.length === 4) continue;
            const geom = feat.geometry;
            if (!geom || !geom.coordinates) continue;
            let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
            function scan(c) {
                if (!c) return;
                if (typeof c[0] === 'number') {
                    const x = c[0], y = c[1];
                    if (x < minX) minX = x;
                    if (x > maxX) maxX = x;
                    if (y < minY) minY = y;
                    if (y > maxY) maxY = y;
                    return;
                }
                for (let i = 0; i < c.length; i++) scan(c[i]);
            }
            scan(geom.coordinates);
            feat.bbox = [minX, minY, maxX, maxY];
        }
    }

    function testPointInFeature(lng, lat, feat) {
        if (!feat || !feat.geometry) return false;
        if (feat.bbox) {
            const [minX, minY, maxX, maxY] = feat.bbox;
            if (lng < minX || lng > maxX || lat < minY || lat > maxY) {
                return false;
            }
        }
        const geom = feat.geometry;
        if (geom.type === 'Polygon') {
            if (!isPointInRing([lng, lat], geom.coordinates[0])) return false;
            for (let k = 1; k < geom.coordinates.length; k++) {
                if (isPointInRing([lng, lat], geom.coordinates[k])) return false;
            }
            return true;
        }
        if (geom.type === 'MultiPolygon') {
            for (const poly of geom.coordinates) {
                if (isPointInRing([lng, lat], poly[0])) {
                    let inHole = false;
                    for (let k = 1; k < poly.length; k++) {
                        if (isPointInRing([lng, lat], poly[k])) {
                            inHole = true;
                            break;
                        }
                    }
                    if (!inHole) return true;
                }
            }
        }
        return false;
    }

    function queryMaritimeFeaturesAtPoint(lng, lat) {
        const results = [];

        // 1. Baseline 12 NM (Territorial Sea)
        if (toggleBaseline && toggleBaseline.checked && baselineData && baselineData.features) {
            for (const feat of baselineData.features) {
                if (testPointInFeature(lng, lat, feat)) {
                    const p = feat.properties || {};
                    results.push({
                        type: 'baseline',
                        badge: '12 NM TERRITORIAL SEA',
                        badgeClass: 'bg-primary/20 text-primary border-primary/40',
                        icon: 'straighten',
                        title: p.GEONAME || '12 NM Territorial Sea',
                        sovereign: p.SOVEREIGN1 || p.TERRITORY1 || 'Coastal State',
                        territory: p.TERRITORY1,
                        areaKm2: p.AREA_KM2,
                        iso: p.ISO_SOV1 || p.ISO_TER1,
                        legal: 'Full coastal state territorial sovereignty (UNCLOS Art. 2 & 3)'
                    });
                    break;
                }
            }
        }

        // 2. EEZ 200 NM (Exclusive Economic Zone)
        if (toggleEez && toggleEez.checked && eezData && eezData.features) {
            for (const feat of eezData.features) {
                if (testPointInFeature(lng, lat, feat)) {
                    const p = feat.properties || {};
                    results.push({
                        type: 'eez',
                        badge: '200 NM EEZ',
                        badgeClass: 'bg-teal-500/20 text-teal-300 border-teal-500/40',
                        icon: 'public',
                        title: p.GEONAME || 'Exclusive Economic Zone',
                        sovereign: p.SOVEREIGN1 || 'Coastal State',
                        territory: p.TERRITORY1,
                        areaKm2: p.AREA_KM2,
                        iso: p.ISO_SOV1 || p.ISO_TER1,
                        legal: 'Sovereign rights over marine living & mineral resources (UNCLOS Art. 56)'
                    });
                    break;
                }
            }
        }

        // 3. FAO Major Fishing Areas
        if (toggleFao && toggleFao.checked && faoData && faoData.features) {
            for (const feat of faoData.features) {
                if (testPointInFeature(lng, lat, feat)) {
                    const p = feat.properties || {};
                    results.push({
                        type: 'fao',
                        badge: `FAO REGION ${p.F_CODE || ''}`,
                        badgeClass: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/40',
                        icon: 'set_meal',
                        title: p.NAME_EN || 'Major Fishing Area',
                        ocean: p.OCEAN,
                        level: p.F_LEVEL || 'Major Fishing Area',
                        legal: 'UN FAO International Fisheries Statistical Reporting Region'
                    });
                    break;
                }
            }
        }

        // 4. IMO SECA / ECA SOx (MARPOL Annex VI Reg 14)
        if (toggleSeca && toggleSeca.checked && secaData && secaData.features) {
            for (const feat of secaData.features) {
                if (testPointInFeature(lng, lat, feat)) {
                    const p = feat.properties || {};
                    results.push({
                        type: 'seca',
                        badge: 'IMO MARPOL SECA',
                        badgeClass: 'bg-sky-500/20 text-sky-300 border-sky-500/40',
                        icon: 'water_drop',
                        title: p.area || 'SOx Emission Control Area',
                        rule: 'Fuel sulfur limit ≤ 0.10% m/m',
                        legal: 'MARPOL Annex VI Reg 14 (Mandatory 0.10% S fuel or approved EGCS scrubber)'
                    });
                    break;
                }
            }
        }

        // 5. IMO ECA NOx (MARPOL Annex VI Reg 13)
        if (toggleEcaNox && toggleEcaNox.checked && ecaNoxData && ecaNoxData.features) {
            for (const feat of ecaNoxData.features) {
                if (testPointInFeature(lng, lat, feat)) {
                    const p = feat.properties || {};
                    results.push({
                        type: 'eca-nox',
                        badge: 'IMO MARPOL ECA NOx',
                        badgeClass: 'bg-cyan-500/20 text-cyan-300 border-cyan-500/40',
                        icon: 'air',
                        title: p.area || 'NOx Emission Control Area',
                        rule: 'Tier III NOx emission standard applies',
                        legal: 'MARPOL Annex VI Reg 13 (Tier III SCR, EGR, or dual-fuel engine required)'
                    });
                    break;
                }
            }
        }

        return results;
    }

    function setOriginCoords(lat, lon) {
        const origLat = document.getElementById('origin-lat');
        const origLon = document.getElementById('origin-lon');
        const origPreset = document.getElementById('origin-preset');
        if (origLat) origLat.value = formatCoordDMS(lat, true);
        if (origLon) origLon.value = formatCoordDMS(lon, false);
        if (origPreset) origPreset.value = 'CUSTOM';
        showToast(`Origin set to: ${formatCoordDMS(lat, true)}, ${formatCoordDMS(lon, false)}`, 'flight_takeoff');
    }

    function setDestCoords(lat, lon) {
        const destLat = document.getElementById('dest-lat');
        const destLon = document.getElementById('dest-lon');
        const destPreset = document.getElementById('dest-preset');
        if (destLat) destLat.value = formatCoordDMS(lat, true);
        if (destLon) destLon.value = formatCoordDMS(lon, false);
        if (destPreset) destPreset.value = 'CUSTOM';
        showToast(`Destination set to: ${formatCoordDMS(lat, true)}, ${formatCoordDMS(lon, false)}`, 'flight_land');
    }

    window.__setOrigin = setOriginCoords;
    window.__setDest = setDestCoords;

    function showTacticalMapPopup(latlng, features) {
        const latFormatted = formatCoordDMS(latlng.lat, true);
        const lonFormatted = formatCoordDMS(latlng.lng, false);

        let cardsHtml = '';
        if (features && features.length > 0) {
            cardsHtml = features.map(f => {
                let details = '';
                if (f.type === 'baseline' || f.type === 'eez') {
                    const sovereign = f.sovereign || 'Coastal State';
                    const territory = f.territory && f.territory !== f.sovereign ? ` (${f.territory})` : '';
                    const iso = f.iso ? ` [${f.iso}]` : '';
                    const area = f.areaKm2 ? `<div><b>Area:</b> ${Math.round(f.areaKm2).toLocaleString()} km²</div>` : '';
                    details = `
                        <div><b>Sovereign / State:</b> <span class="text-on-surface font-semibold">${sovereign}${territory}${iso}</span></div>
                        ${area}
                        <div class="text-[9px] text-on-surface-variant/80 italic mt-0.5">${f.legal}</div>
                    `;
                } else if (f.type === 'fao') {
                    details = `
                        <div><b>Ocean Basin:</b> <span class="text-on-surface font-semibold">${f.ocean || 'Global Oceans'}</span></div>
                        <div><b>Classification:</b> ${f.level}</div>
                        <div class="text-[9px] text-on-surface-variant/80 italic mt-0.5">${f.legal}</div>
                    `;
                } else if (f.type === 'seca' || f.type === 'eca-nox') {
                    details = `
                        <div class="text-on-surface font-semibold">${f.rule}</div>
                        <div class="text-[9px] text-on-surface-variant/80 italic mt-0.5">${f.legal}</div>
                    `;
                }

                return `
                    <div class="p-2 rounded-sm bg-surface-container-low border border-surface-container-high/60 flex flex-col gap-1">
                        <div class="flex items-center justify-between border-b border-surface-container/60 pb-1">
                            <span class="font-bold text-on-surface flex items-center gap-1">
                                <span class="material-symbols-outlined text-[13px] text-primary">${f.icon}</span>
                                <span>${f.title}</span>
                            </span>
                            <span class="text-[9px] px-1 py-0.2 rounded-sm border font-bold ${f.badgeClass}">${f.badge}</span>
                        </div>
                        ${details}
                    </div>
                `;
            }).join('');
        } else {
            const anyActive = (toggleBaseline && toggleBaseline.checked) ||
                              (toggleEez && toggleEez.checked) ||
                              (toggleFao && toggleFao.checked) ||
                              (toggleSeca && toggleSeca.checked) ||
                              (toggleEcaNox && toggleEcaNox.checked);

            const msg = anyActive
                ? 'No jurisdiction boundaries at this coordinate (Open Ocean / High Seas).'
                : 'Turn on Baseline, EEZ, FAO, or ECA overlays in the Layers panel to inspect maritime boundaries at any position.';

            cardsHtml = `
                <div class="p-2 rounded-sm bg-surface-container-low border border-surface-container-high/60 text-on-surface-variant text-[10px]">
                    <div class="font-semibold text-on-surface flex items-center gap-1">
                        <span class="material-symbols-outlined text-[14px] text-tertiary">water</span>
                        <span>International Waters / Open Sea</span>
                    </div>
                    <div class="text-[9px] text-on-surface-variant/80 mt-1">${msg}</div>
                </div>
            `;
        }

        const popupHtml = `
            <div class="p-2.5 font-mono text-[10px] leading-tight select-text flex flex-col gap-2 w-[290px]">
                <div class="flex items-center justify-between border-b border-surface-container-high pb-1.5">
                    <span class="font-bold text-primary flex items-center gap-1">
                        <span class="material-symbols-outlined text-[15px]">explore</span>
                        <span>MARITIME SECTOR</span>
                    </span>
                    <span class="text-[9px] text-on-surface-variant">${latFormatted}, ${lonFormatted}</span>
                </div>
                <div class="flex flex-col gap-1.5 max-h-56 overflow-y-auto custom-scroll">
                    ${cardsHtml}
                </div>
                <div class="flex items-center gap-1.5 pt-1 border-t border-surface-container-high">
                    <button type="button" class="btn-set-popup-origin flex-1 py-1 px-2 rounded-sm bg-primary/20 hover:bg-primary/30 text-primary border border-primary/40 text-[9px] font-semibold text-center cursor-pointer transition-colors" data-lat="${latlng.lat}" data-lng="${latlng.lng}">
                        Set Origin
                    </button>
                    <button type="button" class="btn-set-popup-dest flex-1 py-1 px-2 rounded-sm bg-secondary/20 hover:bg-secondary/30 text-secondary border border-secondary/40 text-[9px] font-semibold text-center cursor-pointer transition-colors" data-lat="${latlng.lat}" data-lng="${latlng.lng}">
                        Set Destination
                    </button>
                </div>
            </div>
        `;

        L.popup({
            className: 'tactical-feature-popup',
            maxWidth: 320,
            offset: [0, -4]
        })
        .setLatLng(latlng)
        .setContent(popupHtml)
        .openOn(map);

        setTimeout(() => {
            const btnOrigin = document.querySelector('.btn-set-popup-origin');
            const btnDest = document.querySelector('.btn-set-popup-dest');
            if (btnOrigin) {
                btnOrigin.addEventListener('click', (ev) => {
                    ev.preventDefault();
                    ev.stopPropagation();
                    const lat = parseFloat(btnOrigin.getAttribute('data-lat'));
                    const lng = parseFloat(btnOrigin.getAttribute('data-lng'));
                    setOriginCoords(lat, lng);
                    map.closePopup();
                });
            }
            if (btnDest) {
                btnDest.addEventListener('click', (ev) => {
                    ev.preventDefault();
                    ev.stopPropagation();
                    const lat = parseFloat(btnDest.getAttribute('data-lat'));
                    const lng = parseFloat(btnDest.getAttribute('data-lng'));
                    setDestCoords(lat, lng);
                    map.closePopup();
                });
            }
        }, 30);
    }

    // =========================================================================
    // 5.0.1 Dynamic Maritime Scale Bar (NM & KM)
    // =========================================================================
    function updateMapScale() {
        const scaleMaxEl = document.getElementById('scale-max');
        const scaleMidEl = document.getElementById('scale-mid');
        const scaleTrackEl = document.getElementById('scale-bar-track');
        const scaleMetricEl = document.getElementById('scale-metric-dist');
        const scaleProjEl = document.getElementById('scale-coords-proj');
        if (!scaleMaxEl || !scaleMidEl || !scaleTrackEl || !map) return;

        const center = map.getCenter();
        const p1 = map.latLngToContainerPoint(center);
        const p2 = L.point(p1.x + 100, p1.y);
        const latlng2 = map.containerPointToLatLng(p2);
        const metersPer100Px = center.distanceTo(latlng2);
        if (!metersPer100Px || isNaN(metersPer100Px) || metersPer100Px <= 0) return;

        const metersPerPx = metersPer100Px / 100;
        const nmPerPx = metersPerPx / 1852;

        const nmSteps = [
            0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 20, 25, 50, 75, 100, 150, 200, 250, 500, 750, 1000, 1500, 2000, 3000, 5000
        ];

        let bestNM = 500;
        let bestPx = 120;
        for (const step of nmSteps) {
            const px = step / nmPerPx;
            if (px >= 65 && px <= 150) {
                bestNM = step;
                bestPx = px;
                break;
            }
        }

        scaleTrackEl.style.width = `${Math.round(bestPx)}px`;
        const midNM = bestNM / 2;

        const fmtNM = (val) => {
            if (val < 0.1) return `${val.toFixed(2)} NM`;
            if (val < 1) return `${val.toFixed(1)} NM`;
            if (val < 10) return `${Number.isInteger(val) ? val : val.toFixed(1)} NM`;
            return `${Math.round(val).toLocaleString()} NM`;
        };

        scaleMidEl.textContent = fmtNM(midNM);
        scaleMaxEl.textContent = fmtNM(bestNM);

        const km = bestNM * 1.852;
        const fmtKM = km >= 10 ? `${Math.round(km).toLocaleString()} KM` : `${km.toFixed(1)} KM`;
        if (scaleMetricEl) scaleMetricEl.textContent = fmtKM;
        if (scaleProjEl) scaleProjEl.textContent = `EPSG:3857 • Z${map.getZoom()}`;
    }

    map.on('zoomend', updateMapScale);
    map.on('moveend', updateMapScale);
    setTimeout(updateMapScale, 100);

    // Top HUD: Live Cursor Readout & Picking on Map
    const cursorCoords = document.getElementById('cursor-coords');
    map.on('mousemove', function (e) {
        if (!cursorCoords) return;
        const latlng = e.latlng.wrap();
        const lat = latlng.lat;
        const lng = latlng.lng;
        const latHem = lat >= 0 ? 'N' : 'S';
        const lonHem = lng >= 0 ? 'E' : 'W';
        cursorCoords.textContent = `Lat: ${Math.abs(lat).toFixed(2)}° ${latHem} | Lon: ${Math.abs(lng).toFixed(2)}° ${lonHem}`;
    });

    map.on('click', function (e) {
        const latlng = e.latlng.wrap();
        if (mapPickingMode === 'origin') {
            const origLat = document.getElementById('origin-lat');
            const origLon = document.getElementById('origin-lon');
            const origPreset = document.getElementById('origin-preset');
            if (origLat) origLat.value = formatCoordDMS(latlng.lat, true);
            if (origLon) origLon.value = formatCoordDMS(latlng.lng, false);
            if (origPreset) origPreset.value = 'CUSTOM';
            stopMapPicking();
            showToast(`Origin set to: ${latlng.lat.toFixed(2)}°, ${latlng.lng.toFixed(2)}°`);
            return;
        }

        if (mapPickingMode === 'dest') {
            const destLat = document.getElementById('dest-lat');
            const destLon = document.getElementById('dest-lon');
            const destPreset = document.getElementById('dest-preset');
            if (destLat) destLat.value = formatCoordDMS(latlng.lat, true);
            if (destLon) destLon.value = formatCoordDMS(latlng.lng, false);
            if (destPreset) destPreset.value = 'CUSTOM';
            stopMapPicking();
            showToast(`Destination set to: ${latlng.lat.toFixed(2)}°, ${latlng.lng.toFixed(2)}°`);
            return;
        }

        // Query active maritime layers at click point and display tactical popup
        const features = queryMaritimeFeaturesAtPoint(latlng.lng, latlng.lat);
        showTacticalMapPopup(latlng, features);
    });

    // 5.1 OWM Composable Weather Matrix (Option A)
    const weatherScaleConfigs = {
        wind: {
            title: 'Wind Speed',
            shortTitle: 'Wind',
            unit: 'KTS',
            gradient: 'linear-gradient(to right, rgba(0,255,255,0.2) 0%, #00ffff 20%, #22c55e 40%, #eab308 65%, #ef4444 85%, #ec4899 100%)',
            ticks: ['0', '15', '30', '45', '60+']
        },
        waves: {
            title: 'Wave Height',
            shortTitle: 'Waves',
            unit: 'M',
            gradient: 'linear-gradient(to right, #38bdf8 0%, #0284c7 25%, #0369a1 50%, #4338ca 75%, #7e22ce 100%)',
            ticks: ['0m', '2m', '4m', '6m', '8m+']
        },
        precip: {
            title: 'Precipitation',
            shortTitle: 'Precip',
            unit: 'MM/H',
            gradient: 'linear-gradient(to right, #bae6fd 0%, #38bdf8 25%, #22c55e 50%, #eab308 75%, #ef4444 90%, #a855f7 100%)',
            ticks: ['0.1', '2', '5', '15', '30+']
        },
        radar: {
            title: 'Weather Radar',
            shortTitle: 'Radar',
            unit: 'MM/H',
            gradient: 'linear-gradient(to right, #bae6fd 0%, #38bdf8 25%, #22c55e 50%, #eab308 75%, #ef4444 90%, #a855f7 100%)',
            ticks: ['0.1', '2', '5', '15', '30+']
        },
        temp: {
            title: 'Temperature',
            shortTitle: 'Temp',
            unit: '°C',
            gradient: 'linear-gradient(to right, #6366f1 0%, #06b6d4 25%, #22c55e 50%, #eab308 75%, #ef4444 100%)',
            ticks: ['-20°', '0°', '15°', '30°', '45°']
        },
        pressure: {
            title: 'Atm Pressure',
            shortTitle: 'Pressure',
            unit: 'HPA',
            gradient: 'linear-gradient(to right, #7e22ce 0%, #3b82f6 25%, #10b981 50%, #eab308 75%, #ef4444 100%)',
            ticks: ['960', '990', '1013', '1030', '1050']
        },
        clouds: {
            title: 'Cloud Coverage',
            shortTitle: 'Clouds',
            unit: '%',
            gradient: 'linear-gradient(to right, rgba(148,163,184,0.1) 0%, rgba(148,163,184,0.4) 40%, #e2e8f0 75%, #ffffff 100%)',
            ticks: ['0%', '25%', '50%', '75%', '100%']
        },
        snow: {
            title: 'Snowfall',
            shortTitle: 'Snow',
            unit: 'MM/H',
            gradient: 'linear-gradient(to right, #e0f2fe 0%, #93c5fd 40%, #60a5fa 70%, #c084fc 100%)',
            ticks: ['0', '2', '5', '10', '20+']
        }
    };

    let selectedWeatherScaleKey = 'wind';

    function updateWeatherScaleDock() {
        const dock = document.getElementById('weather-scale-dock');
        if (!dock) return;

        const masterEnabled = Boolean(toggleOwm && toggleOwm.checked && OWM_API_KEY);
        if (!masterEnabled || activeWeatherLayers.size === 0) {
            dock.classList.add('hidden');
            dock.classList.remove('flex');
            return;
        }

        dock.classList.remove('hidden');
        dock.classList.add('flex');

        if (!activeWeatherLayers.has(selectedWeatherScaleKey)) {
            selectedWeatherScaleKey = Array.from(activeWeatherLayers)[0];
        }

        const cfg = weatherScaleConfigs[selectedWeatherScaleKey] || weatherScaleConfigs.wind;
        const titleEl = document.getElementById('weather-scale-title');
        const unitEl = document.getElementById('weather-scale-unit');
        const gradientEl = document.getElementById('weather-scale-gradient');
        const ticksEl = document.getElementById('weather-scale-ticks');
        const tabsEl = document.getElementById('weather-scale-tabs');

        if (titleEl) titleEl.textContent = cfg.title;
        if (unitEl) unitEl.textContent = cfg.unit;
        if (gradientEl) gradientEl.style.background = cfg.gradient;

        if (ticksEl && cfg.ticks) {
            ticksEl.innerHTML = cfg.ticks.map(t => `<span>${t}</span>`).join('');
        }

        if (tabsEl) {
            if (activeWeatherLayers.size > 1) {
                tabsEl.classList.remove('hidden');
                tabsEl.classList.add('flex', 'flex-wrap');
                tabsEl.innerHTML = '';
                activeWeatherLayers.forEach(key => {
                    const layerCfg = weatherScaleConfigs[key];
                    if (!layerCfg) return;
                    const tabBtn = document.createElement('button');
                    tabBtn.type = 'button';
                    const isActive = key === selectedWeatherScaleKey;
                    tabBtn.setAttribute('role', 'tab');
                    tabBtn.setAttribute('aria-selected', isActive ? 'true' : 'false');
                    tabBtn.className = isActive
                        ? 'px-1.5 py-0.5 rounded-sm bg-primary-container text-on-primary-container font-semibold transition-colors cursor-pointer'
                        : 'px-1.5 py-0.5 rounded-sm text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high/50 transition-colors cursor-pointer';
                    tabBtn.textContent = layerCfg.shortTitle || layerCfg.title;
                    tabBtn.addEventListener('click', (e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        selectedWeatherScaleKey = key;
                        updateWeatherScaleDock();
                    });
                    tabsEl.appendChild(tabBtn);
                });
            } else {
                tabsEl.classList.add('hidden');
                tabsEl.classList.remove('flex');
                tabsEl.innerHTML = '';
            }
        }
    }

    function updateWeatherLayersOnMap() {
        const masterEnabled = Boolean(toggleOwm && toggleOwm.checked);
        Object.entries(weatherSublayers).forEach(([key, layer]) => {
            if (masterEnabled && activeWeatherLayers.has(key)) {
                if (!map.hasLayer(layer)) map.addLayer(layer);
            } else {
                if (map.hasLayer(layer)) map.removeLayer(layer);
            }
        });
    }

    function updateWeatherButtonsUI() {
        document.querySelectorAll('.weather-btn').forEach(btn => {
            const key = btn.getAttribute('data-weather-layer');
            if (activeWeatherLayers.has(key)) {
                btn.className = 'weather-btn active px-1 py-0.5 rounded-sm bg-primary-container text-on-primary-container font-semibold transition-colors text-center cursor-pointer';
            } else {
                btn.className = 'weather-btn px-1 py-0.5 rounded-sm text-on-surface-variant hover:text-on-surface transition-colors text-center cursor-pointer';
            }
        });
    }

    if (toggleOwm) {
        toggleOwm.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (!OWM_API_KEY) {
                    showToast('OpenWeatherMap API Key not configured', 'warning');
                } else {
                    if (activeWeatherLayers.size === 0) activeWeatherLayers.add('wind');
                    updateWeatherButtonsUI();
                    updateWeatherLayersOnMap();
                    showToast('Weather radar layer activated');
                }
            } else {
                updateWeatherLayersOnMap();
            }
            updateActiveLayerBadge();
            updateWeatherScaleDock();
        });
    }

    document.querySelectorAll('.weather-btn').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            const key = btn.getAttribute('data-weather-layer');
            if (!key || !weatherSublayers[key]) return;

            if (activeWeatherLayers.has(key)) {
                activeWeatherLayers.delete(key);
            } else {
                activeWeatherLayers.add(key);
                selectedWeatherScaleKey = key;
                if (toggleOwm && !toggleOwm.checked) {
                    toggleOwm.checked = true;
                }
            }

            updateWeatherButtonsUI();
            updateWeatherLayersOnMap();
            updateActiveLayerBadge();
            updateWeatherScaleDock();
            showToast(`Weather overlay: ${btn.textContent.trim()} ${activeWeatherLayers.has(key) ? 'activated' : 'deactivated'}`, 'cloud');
        });
    });
    updateWeatherButtonsUI();
    updateWeatherScaleDock();

    // 5.2 Marnet Grid Vector Tiles (Unchecked by default)
    function loadMarnetGrid() {
        if (marnetLayer) {
            map.addLayer(marnetLayer);
            return;
        }
        if (marnetLoading) return;
        marnetLoading = true;

        fetch('/network')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(geojson => {
                marnetLoading = false;
                if (typeof L.geoJson.vt === 'function') {
                    marnetLayer = L.geoJson.vt(geojson, {
                        maxZoom: 18,
                        tolerance: 4,
                        style: () => ({
                            color: '#0ea5e9',
                            weight: 1,
                            opacity: 0.35
                        })
                    });
                } else {
                    marnetLayer = L.geoJSON(geojson, {
                        style: () => ({
                            color: '#0ea5e9',
                            weight: 1,
                            opacity: 0.35
                        })
                    });
                }
                if (toggleMarnet && toggleMarnet.checked) {
                    map.addLayer(marnetLayer);
                }
            })
            .catch(err => {
                marnetLoading = false;
                console.warn('Marnet network grid unavailable:', err);
            });
    }

    if (toggleMarnet) {
        toggleMarnet.addEventListener('change', (e) => {
            if (e.target.checked) {
                loadMarnetGrid();
                showToast('Marnet Routing Mesh activated');
            } else if (marnetLayer) {
                map.removeLayer(marnetLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleMarnet.checked) loadMarnetGrid();
    }

    // 5.3 HRA (High Risk Piracy Area BMP5)
    function createHraLayer() {
        const hraCoords = [
            [15.0, 42.0],
            [12.0, 44.0],
            [10.0, 51.0],
            [5.0, 50.0],
            [-10.0, 41.0],
            [-10.0, 60.0],
            [12.0, 65.0],
            [22.0, 65.0],
            [25.0, 61.0],
            [24.0, 57.0],
            [17.0, 54.0],
            [13.0, 48.0],
            [15.0, 42.0]
        ];
        hraLayer = L.polygon(hraCoords, {
            color: '#f59e0b',
            weight: 1.5,
            opacity: 0.8,
            fillColor: '#f59e0b',
            fillOpacity: 0.12,
            dashArray: '4, 4'
        }).bindPopup('<b>High Risk Area (HRA)</b><br>BMP5 Piracy Risk Zone<br>Western Indian Ocean &amp; Gulf of Aden');
    }

    if (toggleHra) {
        createHraLayer();
        if (toggleHra.checked && hraLayer) hraLayer.addTo(map);
        toggleHra.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (hraLayer) map.addLayer(hraLayer);
                showToast('HRA Piracy corridor displayed');
            } else {
                if (hraLayer) map.removeLayer(hraLayer);
            }
            updateActiveLayerBadge();
        });
    }

    // 5.4 Piracy Incidents (ASAM Events with Marker Cluster)
    function loadPiracyLayer() {
        if (piracyLayer) {
            map.addLayer(piracyLayer);
            return;
        }
        if (piracyLoading) return;
        piracyLoading = true;
        showToast('Streaming piracy incidents feed...', 'download');

        fetch('/layers/piracy')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(data => {
                piracyLoading = false;
                piracyLayer = L.markerClusterGroup({
                    chunkedLoading: true,
                    maxClusterRadius: 45,
                    iconCreateFunction: function (cluster) {
                        const count = cluster.getChildCount();
                        return L.divIcon({
                            html: `<div><span>${count}</span></div>`,
                            className: 'marker-cluster piracy-cluster',
                            iconSize: L.point(34, 34)
                        });
                    }
                });

                const geojson = L.geoJSON(data, {
                    pointToLayer: function (feature, latlng) {
                        const pin = L.divIcon({
                            className: 'piracy-marker-pin',
                            html: '<span class="material-symbols-outlined text-[13px]">warning</span>',
                            iconSize: [20, 20],
                            iconAnchor: [10, 10]
                        });
                        return L.marker(latlng, { icon: pin });
                    },
                    onEachFeature: function (feature, layer) {
                        const p = feature.properties || {};
                        const dateStr = p.date ? p.date.slice(0, 10) : 'N/A';
                        const victim = p.victim || 'Vessel in transit';
                        const aggressor = p.aggressor || 'Armed Pirates';
                        const narrative = p.narrative || 'Incident reported to Maritime Security Centre.';
                        const subreg = p.subreg || 'ASAM';
                        const popupHtml = `
                            <div class="p-2 font-mono text-[10px] leading-tight select-text flex flex-col gap-1 w-[260px]">
                                <div class="flex items-center justify-between border-b border-error/40 pb-1">
                                    <span class="font-bold text-error flex items-center gap-1"><span class="material-symbols-outlined text-[14px]">e911_emergency</span>PIRACY INCIDENT</span>
                                    <span class="text-[9px] px-1 py-0.2 rounded-sm bg-error/20 text-error font-bold">${subreg}</span>
                                </div>
                                <div><b>Date:</b> ${dateStr}</div>
                                <div><b>Target:</b> ${victim}</div>
                                <div><b>Aggressor:</b> <span class="text-error font-semibold">${aggressor}</span></div>
                                <div class="p-1 rounded-sm bg-surface-container-low text-[9px] mt-0.5 border border-surface-container-high/60 max-h-24 overflow-y-auto custom-scroll">${narrative}</div>
                            </div>
                        `;
                        layer.bindPopup(popupHtml, { className: 'piracy-popup', maxWidth: 280 });
                    }
                });

                piracyLayer.addLayer(geojson);
                if (togglePiracy && togglePiracy.checked) {
                    map.addLayer(piracyLayer);
                }
                showToast('Piracy incidents feed active');
            })
            .catch(err => {
                piracyLoading = false;
                console.warn('Piracy incidents unavailable:', err);
                showToast('Failed to load piracy incidents', 'warning');
            });
    }

    if (togglePiracy) {
        togglePiracy.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (piracyLayer) map.addLayer(piracyLayer);
                else loadPiracyLayer();
            } else if (piracyLayer) {
                map.removeLayer(piracyLayer);
            }
            updateActiveLayerBadge();
        });
        if (togglePiracy.checked) loadPiracyLayer();
    }

    // 5.5 World Seaports (WPI with Marker Cluster)
    function loadSeaportsLayer() {
        if (seaportsLayer) {
            map.addLayer(seaportsLayer);
            return;
        }
        if (seaportsLoading) return;
        seaportsLoading = true;
        showToast('Streaming World Seaport index...', 'download');

        fetch('/layers/seaports')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(data => {
                seaportsLoading = false;
                seaportsLayer = L.markerClusterGroup({
                    chunkedLoading: true,
                    maxClusterRadius: 50,
                    iconCreateFunction: function (cluster) {
                        const count = cluster.getChildCount();
                        return L.divIcon({
                            html: `<div><span>${count}</span></div>`,
                            className: 'marker-cluster seaport-cluster',
                            iconSize: L.point(34, 34)
                        });
                    }
                });

                const geojson = L.geoJSON(data, {
                    pointToLayer: function (feature, latlng) {
                        const pin = L.divIcon({
                            className: 'seaport-marker-pin',
                            html: '<span class="material-symbols-outlined text-[13px]">anchor</span>',
                            iconSize: [20, 20],
                            iconAnchor: [10, 10]
                        });
                        return L.marker(latlng, { icon: pin });
                    },
                    onEachFeature: function (feature, layer) {
                        const p = feature.properties || {};
                        const portName = p.PORT_NAME || 'Seaport';
                        const country = p.COUNTRY || 'N/A';
                        const harborSize = p.HARBORSIZE || 'Medium';
                        const harborType = p.HARBORTYPE || 'Coastal';
                        const depth = p.CHAN_DEPTH ? `${p.CHAN_DEPTH}m` : '--';
                        const maxDraft = p.MAX_DRAFT ? `${p.MAX_DRAFT}m` : '--';
                        const indexNo = p.INDEX_NO || 'WPI';
                        const popupHtml = `
                            <div class="p-2 font-mono text-[10px] leading-tight select-text flex flex-col gap-1 w-[250px]">
                                <div class="flex items-center justify-between border-b border-primary/40 pb-1">
                                    <span class="font-bold text-primary flex items-center gap-1"><span class="material-symbols-outlined text-[14px]">anchor</span>${portName}</span>
                                    <span class="text-[9px] px-1 py-0.2 rounded-sm bg-primary/20 text-primary font-bold">#${indexNo}</span>
                                </div>
                                <div><b>Country:</b> ${country}</div>
                                <div><b>Harbor:</b> ${harborSize} / ${harborType}</div>
                                <div><b>Channel / Max Draft:</b> ${depth} / ${maxDraft}</div>
                            </div>
                        `;
                        layer.bindPopup(popupHtml, { className: 'seaport-popup', maxWidth: 280 });
                    }
                });

                seaportsLayer.addLayer(geojson);
                if (toggleSeaports && toggleSeaports.checked) {
                    map.addLayer(seaportsLayer);
                }
                showToast('World Seaport Index active');
            })
            .catch(err => {
                seaportsLoading = false;
                console.warn('World seaports unavailable:', err);
                showToast('Failed to load seaport index', 'warning');
            });
    }

    if (toggleSeaports) {
        toggleSeaports.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (seaportsLayer) map.addLayer(seaportsLayer);
                else loadSeaportsLayer();
            } else if (seaportsLayer) {
                map.removeLayer(seaportsLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleSeaports.checked) loadSeaportsLayer();
    }

    // 5.6 SECA / ECA SOx Tactical Layer (IMO MARPOL Annex VI Reg 14)
    function loadSecaLayer() {
        if (secaLayer) {
            map.addLayer(secaLayer);
            return;
        }
        if (secaLoading) return;
        secaLoading = true;

        fetch('/layers/eca-sox')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(geojson => {
                secaLoading = false;
                secaData = geojson;
                prepareGeoJsonBBoxes(secaData);
                if (typeof L.geoJson.vt === 'function') {
                    secaLayer = L.geoJson.vt(geojson, {
                        maxZoom: 18,
                        tolerance: 4,
                        style: () => ({
                            color: '#38bdf8',
                            weight: 1.5,
                            opacity: 0.85,
                            fillColor: '#38bdf8',
                            fillOpacity: 0.12
                        })
                    });
                } else {
                    secaLayer = L.geoJSON(geojson, {
                        style: () => ({
                            color: '#38bdf8',
                            weight: 1.5,
                            opacity: 0.85,
                            fillColor: '#38bdf8',
                            fillOpacity: 0.12,
                            dashArray: '3, 3'
                        })
                    });
                }
                if (toggleSeca && toggleSeca.checked) {
                    map.addLayer(secaLayer);
                }
                showToast('SECA / ECA SOx Zones displayed');
            })
            .catch(err => {
                secaLoading = false;
                console.warn('Emission Control Areas unavailable:', err);
            });
    }

    if (toggleSeca) {
        toggleSeca.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (secaLayer) map.addLayer(secaLayer);
                else loadSecaLayer();
            } else if (secaLayer) {
                map.removeLayer(secaLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleSeca.checked) loadSecaLayer();
    }

    // 5.7 ECA NOx Tier III Tactical Layer
    function loadEcaNoxLayer() {
        if (ecaNoxLayer) {
            map.addLayer(ecaNoxLayer);
            return;
        }
        if (ecaNoxLoading) return;
        ecaNoxLoading = true;

        fetch('/layers/eca-nox')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(geojson => {
                ecaNoxLoading = false;
                ecaNoxData = geojson;
                prepareGeoJsonBBoxes(ecaNoxData);
                if (typeof L.geoJson.vt === 'function') {
                    ecaNoxLayer = L.geoJson.vt(geojson, {
                        maxZoom: 18,
                        tolerance: 4,
                        style: () => ({
                            color: '#a855f7',
                            weight: 1.5,
                            opacity: 0.85,
                            fillColor: '#a855f7',
                            fillOpacity: 0.12
                        })
                    });
                } else {
                    ecaNoxLayer = L.geoJSON(geojson, {
                        style: () => ({
                            color: '#a855f7',
                            weight: 1.5,
                            opacity: 0.85,
                            fillColor: '#a855f7',
                            fillOpacity: 0.12,
                            dashArray: '3, 3'
                        })
                    });
                }
                if (toggleEcaNox && toggleEcaNox.checked) {
                    map.addLayer(ecaNoxLayer);
                }
                showToast('ECA NOx Tier III Zones displayed');
            })
            .catch(err => {
                ecaNoxLoading = false;
                console.warn('ECA NOx layer unavailable:', err);
            });
    }

    if (toggleEcaNox) {
        toggleEcaNox.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (ecaNoxLayer) map.addLayer(ecaNoxLayer);
                else loadEcaNoxLayer();
            } else if (ecaNoxLayer) {
                map.removeLayer(ecaNoxLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleEcaNox.checked) loadEcaNoxLayer();
    }

    // 5.8 Territorial Sea Baseline & 12 NM
    function loadBaselineLayer() {
        if (baselineLayer) {
            map.addLayer(baselineLayer);
            return;
        }
        if (baselineLoading) return;
        baselineLoading = true;
        showToast('Streaming 12 NM Baseline layer...', 'download');

        fetch('/layers/baseline')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(geojson => {
                baselineLoading = false;
                baselineData = geojson;
                prepareGeoJsonBBoxes(baselineData);
                if (typeof L.geoJson.vt === 'function') {
                    baselineLayer = L.geoJson.vt(geojson, {
                        maxZoom: 18,
                        tolerance: 4,
                        style: () => ({
                            color: '#0284c7',
                            weight: 1.5,
                            opacity: 0.85,
                            fillColor: '#0284c7',
                            fillOpacity: 0.05
                        })
                    });
                } else {
                    baselineLayer = L.geoJSON(geojson, {
                        style: () => ({
                            color: '#0284c7',
                            weight: 1.5,
                            opacity: 0.85,
                            fillColor: '#0284c7',
                            fillOpacity: 0.05,
                            dashArray: '4, 4'
                        })
                    });
                }
                if (toggleBaseline && toggleBaseline.checked) {
                    map.addLayer(baselineLayer);
                }
                showToast('12 NM Territorial Baseline displayed');
            })
            .catch(err => {
                baselineLoading = false;
                console.warn('Baseline layer unavailable:', err);
            });
    }

    if (toggleBaseline) {
        toggleBaseline.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (baselineLayer) map.addLayer(baselineLayer);
                else loadBaselineLayer();
            } else if (baselineLayer) {
                map.removeLayer(baselineLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleBaseline.checked) loadBaselineLayer();
    }

    // 5.9 Exclusive Economic Zones (EEZ)
    function loadEezLayer() {
        if (eezLayer) {
            map.addLayer(eezLayer);
            return;
        }
        if (eezLoading) return;
        eezLoading = true;
        showToast('Streaming Exclusive Economic Zones...', 'download');

        fetch('/layers/eez')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(geojson => {
                eezLoading = false;
                eezData = geojson;
                prepareGeoJsonBBoxes(eezData);
                if (typeof L.geoJson.vt === 'function') {
                    eezLayer = L.geoJson.vt(geojson, {
                        maxZoom: 18,
                        tolerance: 4,
                        style: () => ({
                            color: '#0d9488',
                            weight: 1.5,
                            opacity: 0.8,
                            fillColor: '#0d9488',
                            fillOpacity: 0.08
                        })
                    });
                } else {
                    eezLayer = L.geoJSON(geojson, {
                        style: () => ({
                            color: '#0d9488',
                            weight: 1.5,
                            opacity: 0.8,
                            fillColor: '#0d9488',
                            fillOpacity: 0.08
                        })
                    });
                }
                if (toggleEez && toggleEez.checked) {
                    map.addLayer(eezLayer);
                }
                showToast('Exclusive Economic Zones displayed');
            })
            .catch(err => {
                eezLoading = false;
                console.warn('EEZ layer unavailable:', err);
            });
    }

    if (toggleEez) {
        toggleEez.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (eezLayer) map.addLayer(eezLayer);
                else loadEezLayer();
            } else if (eezLayer) {
                map.removeLayer(eezLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleEez.checked) loadEezLayer();
    }

    // 5.10 FAO Major Fishing Areas
    function loadFaoLayer() {
        if (faoLayer) {
            map.addLayer(faoLayer);
            return;
        }
        if (faoLoading) return;
        faoLoading = true;

        fetch('/layers/fao')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(geojson => {
                faoLoading = false;
                faoData = geojson;
                prepareGeoJsonBBoxes(faoData);
                if (typeof L.geoJson.vt === 'function') {
                    faoLayer = L.geoJson.vt(geojson, {
                        maxZoom: 18,
                        tolerance: 4,
                        style: () => ({
                            color: '#10b981',
                            weight: 1,
                            opacity: 0.75,
                            fillColor: '#10b981',
                            fillOpacity: 0.05
                        })
                    });
                } else {
                    faoLayer = L.geoJSON(geojson, {
                        style: () => ({
                            color: '#10b981',
                            weight: 1,
                            opacity: 0.75,
                            fillColor: '#10b981',
                            fillOpacity: 0.05
                        })
                    });
                }
                if (toggleFao && toggleFao.checked) {
                    map.addLayer(faoLayer);
                }
                showToast('FAO Fishing Areas displayed');
            })
            .catch(err => {
                faoLoading = false;
                console.warn('FAO layer unavailable:', err);
            });
    }

    if (toggleFao) {
        toggleFao.addEventListener('change', (e) => {
            if (e.target.checked) {
                if (faoLayer) map.addLayer(faoLayer);
                else loadFaoLayer();
            } else if (faoLayer) {
                map.removeLayer(faoLayer);
            }
            updateActiveLayerBadge();
        });
        if (toggleFao.checked) loadFaoLayer();
    }

    updateActiveLayerBadge();

    // =========================================================================
    // 6. Dynamic Passages, Avoidance Tags & Metadata Popover
    // =========================================================================
    const PASSAGE_LABELS = {
        'suez': 'Suez Canal',
        'malacca': 'Malacca Strait',
        'babelmandeb': 'Bab-el-Mandeb',
        'hormuz': 'Strait of Hormuz',
        'panama': 'Panama Canal',
        'gibraltar': 'Gibraltar',
        'bosphorus': 'Bosphorus / Dardanelles',
        'kiel': 'Kiel Canal',
        'oresund': 'Øresund / The Sound',
        'corinth': 'Corinth Canal',
        'dover': 'Strait of Dover',
        'tsugaru': 'Tsugaru Strait',
        'sunda': 'Sunda Strait',
        'lombat': 'Lombok Strait',
        'magellan': 'Strait of Magellan',
        'cape_horn': 'Cape Horn',
        'good_hope': 'Cape of Good Hope',
        'northwest': 'Northwest Passage (Arctic)',
        'northeast': 'Northeast Passage (Arctic)'
    };

    const avoidTagsContainer = document.getElementById('avoid-tags-container');
    const avoidCountBadge = document.getElementById('avoid-count-badge');
    const collapsedAvoidVal = document.getElementById('collapsed-avoid-val');

    function updateAvoidBadge() {
        const activeCount = document.querySelectorAll('.avoid-pill.active').length;
        if (avoidCountBadge) {
            if (activeCount === 0) {
                avoidCountBadge.textContent = '0 Blocked';
                avoidCountBadge.className = 'font-mono text-[9px] px-1.5 py-0.2 rounded-sm bg-surface-container text-on-surface-variant border border-surface-variant';
            } else {
                avoidCountBadge.textContent = `${activeCount} Active ${activeCount === 1 ? 'Block' : 'Blocks'}`;
                avoidCountBadge.className = 'font-mono text-[9px] px-1.5 py-0.2 rounded-sm bg-warning/20 text-warning border border-warning/40 font-semibold';
            }
        }
        if (collapsedAvoidVal) {
            collapsedAvoidVal.textContent = activeCount > 0 ? `${activeCount} BLK` : 'AVOID';
        }
    }

    function toggleAvoidPill(pill) {
        const isActive = pill.classList.contains('active');
        const icon = pill.querySelector('.material-symbols-outlined');

        if (isActive) {
            pill.classList.remove('active');
            pill.className = 'avoid-pill px-2 py-1 rounded-sm text-[10px] font-mono flex items-center gap-1 transition-all border bg-surface-container-lowest border-surface-container-high text-on-surface hover:border-surface-variant cursor-pointer';
            if (icon) {
                icon.textContent = 'circle';
                icon.className = 'material-symbols-outlined text-[12px] opacity-40';
            }
        } else {
            pill.classList.add('active');
            pill.className = 'avoid-pill active px-2 py-1 rounded-sm text-[10px] font-mono flex items-center gap-1 transition-all border bg-warning/20 border-warning text-warning shadow-sm font-semibold cursor-pointer';
            if (icon) {
                icon.textContent = 'do_not_disturb_on';
                icon.className = 'material-symbols-outlined text-[12px]';
            }
        }
        updateAvoidBadge();
        const passageKey = pill.getAttribute('data-avoid');
        showToast(`Passage constraint: ${PASSAGE_LABELS[passageKey] || passageKey} ${isActive ? 'cleared' : 'blocked'}`);
    }

    function loadPassagesMetadata() {
        fetch('/passages')
            .then(res => {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(data => {
                // Update system metadata card
                const sysEngine = document.getElementById('sys-info-engine');
                const sysMesh = document.getElementById('sys-info-mesh');
                const sysNodes = document.getElementById('sys-info-nodes-edges');
                if (sysEngine && data.engine) sysEngine.textContent = data.engine;
                if (sysMesh && data.mesh) sysMesh.textContent = data.mesh;
                if (sysNodes && data.nodes) {
                    sysNodes.textContent = `${Number(data.nodes).toLocaleString()} / ${Number(data.edges || 118400).toLocaleString()}`;
                }

                // Render dynamic avoid pills if supported, preserving any active selections
                if (data.passages && Array.isArray(data.passages) && avoidTagsContainer) {
                    const activeAvoids = new Set();
                    avoidTagsContainer.querySelectorAll('.avoid-pill.active').forEach(p => {
                        const code = p.getAttribute('data-avoid');
                        if (code) activeAvoids.add(code.toLowerCase());
                    });
                    const urlParams = new URLSearchParams(window.location.search);
                    if (urlParams.has('avoid')) {
                        urlParams.get('avoid').split(',').map(s => s.trim().toLowerCase()).filter(Boolean).forEach(c => activeAvoids.add(c));
                    }

                    avoidTagsContainer.innerHTML = '';
                    data.passages.forEach(pass => {
                        const passKey = pass.toLowerCase();
                        const isActive = activeAvoids.has(passKey);
                        const btn = document.createElement('button');
                        btn.type = 'button';
                        btn.setAttribute('data-avoid', pass);
                        if (isActive) {
                            btn.className = 'avoid-pill active px-2 py-1 rounded-sm text-[10px] font-mono flex items-center gap-1 transition-all border bg-warning/20 border-warning text-warning shadow-sm font-semibold cursor-pointer';
                            btn.innerHTML = `<span class="material-symbols-outlined text-[12px]">do_not_disturb_on</span><span>${PASSAGE_LABELS[pass] || pass}</span>`;
                        } else {
                            btn.className = 'avoid-pill px-2 py-1 rounded-sm text-[10px] font-mono flex items-center gap-1 transition-all border bg-surface-container-lowest border-surface-container-high text-on-surface hover:border-surface-variant cursor-pointer';
                            btn.innerHTML = `<span class="material-symbols-outlined text-[12px] opacity-40">circle</span><span>${PASSAGE_LABELS[pass] || pass}</span>`;
                        }
                        btn.addEventListener('click', () => toggleAvoidPill(btn));
                        avoidTagsContainer.appendChild(btn);
                    });
                    updateAvoidBadge();
                }
            })
            .catch(err => {
                console.warn('Failed to load passages metadata:', err);
                // Bind existing static pills if already in DOM
                document.querySelectorAll('.avoid-pill').forEach(pill => {
                    pill.addEventListener('click', () => toggleAvoidPill(pill));
                });
            });
    }

    loadPassagesMetadata();

    // =========================================================================
    // 7. Routing Algorithm Selector (Dijkstra vs A*)
    // =========================================================================
    const btnDijkstra = document.getElementById('algo-btn-dijkstra');
    const btnAstar = document.getElementById('algo-btn-astar');
    const algoPerfBadge = document.getElementById('algo-perf-badge');
    const algoExplanation = document.getElementById('algo-explanation');

    function setAlgorithm(algo) {
        currentAlgorithm = algo;
        if (algo === 'dijkstra') {
            if (btnDijkstra) {
                btnDijkstra.className = 'algo-pill-active py-1.5 px-2 rounded-sm text-[11px] font-mono font-medium flex items-center justify-center gap-1.5 transition-all bg-primary-container text-on-primary-container shadow-sm cursor-pointer';
            }
            if (btnAstar) {
                btnAstar.className = 'py-1.5 px-2 rounded-sm text-[11px] font-mono font-medium flex items-center justify-center gap-1.5 transition-all text-on-surface-variant hover:text-on-surface hover:bg-surface-container cursor-pointer';
            }
            if (algoPerfBadge) {
                algoPerfBadge.textContent = 'O(E + V log V)';
                algoPerfBadge.className = 'font-mono text-[9px] px-1.5 py-0.5 rounded-sm bg-surface-container text-secondary border border-surface-variant';
            }
            if (algoExplanation) {
                algoExplanation.textContent = 'Standard Dijkstra traverses lowest cumulative nautical weight across graph nodes. Guarantees global optimum.';
            }
            showToast('Algorithm set to Dijkstra Engine (Global Optimum)');
        } else {
            if (btnAstar) {
                btnAstar.className = 'algo-pill-active py-1.5 px-2 rounded-sm text-[11px] font-mono font-medium flex items-center justify-center gap-1.5 transition-all bg-primary-container text-on-primary-container shadow-sm cursor-pointer';
            }
            if (btnDijkstra) {
                btnDijkstra.className = 'py-1.5 px-2 rounded-sm text-[11px] font-mono font-medium flex items-center justify-center gap-1.5 transition-all text-on-surface-variant hover:text-on-surface hover:bg-surface-container cursor-pointer';
            }
            if (algoPerfBadge) {
                algoPerfBadge.textContent = 'A* Great-Circle Heuristic';
                algoPerfBadge.className = 'font-mono text-[9px] px-1.5 py-0.5 rounded-sm bg-surface-container text-tertiary border border-surface-variant';
            }
            if (algoExplanation) {
                algoExplanation.textContent = 'A* applies haversine Euclidean distance heuristic targeting destination port for accelerated search convergence.';
            }
            showToast('Algorithm set to A* (Great-Circle Heuristic)');
        }
    }

    if (btnDijkstra) btnDijkstra.addEventListener('click', () => setAlgorithm('dijkstra'));
    if (btnAstar) btnAstar.addEventListener('click', () => setAlgorithm('astar'));

    // =========================================================================
    // 8. Origin & Destination Presets, Coordinate Inputs & Autocomplete
    // =========================================================================
    const PRESET_COORDS = {
        'NLRTM': { name: 'Rotterdam, Netherlands (NLRTM)', lat: 51.95, lon: 4.05 },
        'BEANR': { name: 'Antwerp-Bruges, Belgium (BEANR)', lat: 51.24, lon: 4.40 },
        'DEHAM': { name: 'Hamburg, Germany (DEHAM)', lat: 53.55, lon: 9.99 },
        'USNYC': { name: 'New York / New Jersey, USA (USNYC)', lat: 40.68, lon: -74.04 },
        'CNSHA': { name: 'Shanghai, China (CNSHA)', lat: 31.23, lon: 121.47 },
        'SGSIN': { name: 'Singapore, Singapore (SGSIN)', lat: 1.26, lon: 103.82 },
        'AEJEA': { name: 'Jebel Ali / Dubai, UAE (AEJEA)', lat: 25.01, lon: 55.06 },
        'JPTYO': { name: 'Tokyo, Japan (JPTYO)', lat: 35.65, lon: 139.75 },
        'USLAX': { name: 'Los Angeles, USA (USLAX)', lat: 33.74, lon: -118.26 }
    };

    /**
     * Resolves a location specifier (coordinates "lat,lon" or port UN/LOCODE / name).
     * Returns { lat, lon, preset, name } or null if unresolved.
     */
    function resolveLocationParam(param) {
        if (!param || typeof param !== 'string') return null;
        const str = param.trim();
        if (!str) return null;

        // 1. Check if coordinate pair: "lat,lon", "lat, lon", "lat;lon", "lat/lon"
        if (str.includes(',') || str.includes(';') || (str.includes('/') && !str.includes(' '))) {
            const parts = str.split(/[,;\/]/);
            if (parts.length >= 2) {
                const lat = parseCoordinate(parts[0], true);
                const lon = parseCoordinate(parts[1], false);
                if (!isNaN(lat) && !isNaN(lon) && Math.abs(lat) <= 90 && Math.abs(lon) <= 180) {
                    let matchedKey = 'CUSTOM';
                    for (const [key, item] of Object.entries(PRESET_COORDS)) {
                        if (Math.abs(item.lat - lat) < 0.05 && Math.abs(item.lon - lon) < 0.05) {
                            matchedKey = key;
                            break;
                        }
                    }
                    return { lat, lon, preset: matchedKey, name: `${lat.toFixed(4)}, ${lon.toFixed(4)}` };
                }
            }
        }

        // 2. Exact match against UN/LOCODE or preset code (e.g., NLRTM, SGSIN)
        const upper = str.toUpperCase();
        if (PRESET_COORDS[upper]) {
            return {
                lat: PRESET_COORDS[upper].lat,
                lon: PRESET_COORDS[upper].lon,
                preset: upper,
                name: PRESET_COORDS[upper].name
            };
        }

        // 3. Match against port name in PRESET_COORDS (e.g. "Rotterdam", "Singapore")
        const lower = str.toLowerCase();
        for (const [key, item] of Object.entries(PRESET_COORDS)) {
            if (item.name.toLowerCase().includes(lower) || key.toLowerCase() === lower) {
                return {
                    lat: item.lat,
                    lon: item.lon,
                    preset: key,
                    name: item.name
                };
            }
        }

        return null;
    }

    const originPreset = document.getElementById('origin-preset');
    const destPreset = document.getElementById('dest-preset');
    const originLatInput = document.getElementById('origin-lat');
    const originLonInput = document.getElementById('origin-lon');
    const destLatInput = document.getElementById('dest-lat');
    const destLonInput = document.getElementById('dest-lon');

    if (originPreset) {
        originPreset.addEventListener('change', (e) => {
            const val = e.target.value;
            if (PRESET_COORDS[val]) {
                const item = PRESET_COORDS[val];
                if (originLatInput) originLatInput.value = formatCoordDMS(item.lat, true);
                if (originLonInput) originLonInput.value = formatCoordDMS(item.lon, false);
            }
        });
    }

    if (destPreset) {
        destPreset.addEventListener('change', (e) => {
            const val = e.target.value;
            if (PRESET_COORDS[val]) {
                const item = PRESET_COORDS[val];
                if (destLatInput) destLatInput.value = formatCoordDMS(item.lat, true);
                if (destLonInput) destLonInput.value = formatCoordDMS(item.lon, false);
            }
        });
    }

    // Autocomplete Dropdowns (GET /ports?search=...)
    function setupPortAutocomplete(inputId, dropdownId, latInputId, lonInputId, presetSelectId) {
        const input = document.getElementById(inputId);
        const dropdown = document.getElementById(dropdownId);
        const latInput = document.getElementById(latInputId);
        const lonInput = document.getElementById(lonInputId);
        const presetSelect = document.getElementById(presetSelectId);

        if (!input || !dropdown) return;

        let debounceTimer = null;

        input.addEventListener('input', () => {
            const query = input.value.trim();
            if (debounceTimer) clearTimeout(debounceTimer);

            if (query.length < 2) {
                dropdown.innerHTML = '';
                dropdown.classList.add('hidden');
                return;
            }

            debounceTimer = setTimeout(() => {
                fetch('/ports?search=' + encodeURIComponent(query))
                    .then(res => res.json())
                    .then(ports => {
                        dropdown.innerHTML = '';
                        if (!ports || ports.length === 0) {
                            dropdown.innerHTML = '<div class="p-2 text-on-surface-variant text-[10px]">No ports found.</div>';
                            dropdown.classList.remove('hidden');
                            return;
                        }

                        ports.slice(0, 10).forEach(p => {
                            const item = document.createElement('div');
                            item.className = 'autocomplete-item flex items-center justify-between text-[10px] p-2 hover:bg-surface-container cursor-pointer border-b border-surface-container-high/40';
                            item.innerHTML = `
                                <div>
                                    <span class="font-bold text-on-surface">${p.port}</span>
                                    <span class="text-on-surface-variant font-normal">(${p.country})</span>
                                    <div class="text-[9px] text-outline font-mono">${p.code || ''} • ${p.latitude.toFixed(2)}°, ${p.longitude.toFixed(2)}°</div>
                                </div>
                                <span class="material-symbols-outlined text-primary text-[14px]">arrow_forward</span>
                            `;
                            item.addEventListener('click', () => {
                                if (latInput) latInput.value = formatCoordDMS(p.latitude, true);
                                if (lonInput) lonInput.value = formatCoordDMS(p.longitude, false);
                                if (presetSelect) presetSelect.value = 'CUSTOM';
                                input.value = `${p.port} (${p.country})`;
                                dropdown.innerHTML = '';
                                dropdown.classList.add('hidden');
                                showToast(`Selected: ${p.port} (${p.country})`);
                            });
                            dropdown.appendChild(item);
                        });
                        dropdown.classList.remove('hidden');
                    })
                    .catch(err => console.warn('Autocomplete fetch error:', err));
            }, 250);
        });

        // Close on blur/click outside
        document.addEventListener('click', (e) => {
            if (!dropdown.contains(e.target) && e.target !== input) {
                dropdown.classList.add('hidden');
            }
        });
    }

    setupPortAutocomplete('origin-search', 'origin-autocomplete', 'origin-lat', 'origin-lon', 'origin-preset');
    setupPortAutocomplete('dest-search', 'dest-autocomplete', 'dest-lat', 'dest-lon', 'dest-preset');

    // "Pick on Map" button handlers
    const btnPickOrigin = document.getElementById('btn-map-pick-origin');
    const btnPickDest = document.getElementById('btn-map-pick-dest');

    function startMapPicking(mode) {
        mapPickingMode = mode;
        document.body.classList.add('cursor-crosshair');
        document.getElementById('map').classList.add('cursor-crosshair');

        if (mode === 'origin') {
            if (btnPickOrigin) btnPickOrigin.classList.add('font-bold', 'text-primary');
            if (btnPickDest) btnPickDest.classList.remove('font-bold', 'text-primary');
            showToast('Click anywhere on chart to pick Origin coordinates...', 'pin_drop');
        } else {
            if (btnPickDest) btnPickDest.classList.add('font-bold', 'text-secondary');
            if (btnPickOrigin) btnPickOrigin.classList.remove('font-bold', 'text-secondary');
            showToast('Click anywhere on chart to pick Destination coordinates...', 'pin_drop');
        }
    }

    function stopMapPicking() {
        mapPickingMode = null;
        document.body.classList.remove('cursor-crosshair');
        document.getElementById('map').classList.remove('cursor-crosshair');
        if (btnPickOrigin) btnPickOrigin.classList.remove('font-bold', 'text-primary');
        if (btnPickDest) btnPickDest.classList.remove('font-bold', 'text-secondary');
    }

    if (btnPickOrigin) {
        btnPickOrigin.addEventListener('click', (e) => {
            e.stopPropagation();
            if (mapPickingMode === 'origin') stopMapPicking();
            else startMapPicking('origin');
        });
    }

    if (btnPickDest) {
        btnPickDest.addEventListener('click', (e) => {
            e.stopPropagation();
            if (mapPickingMode === 'dest') stopMapPicking();
            else startMapPicking('dest');
        });
    }

    // Origin/Destination Swap Button (#btn-reverse)
    const btnReverse = document.getElementById('btn-reverse');
    if (btnReverse) {
        btnReverse.addEventListener('click', () => {
            const origSelect = document.getElementById('origin-preset');
            const destSelect = document.getElementById('dest-preset');
            const origLat = document.getElementById('origin-lat');
            const destLat = document.getElementById('dest-lat');
            const origLon = document.getElementById('origin-lon');
            const destLon = document.getElementById('dest-lon');
            const origSearch = document.getElementById('origin-search');
            const destSearch = document.getElementById('dest-search');

            if (origSelect && destSelect) {
                const tempSelect = origSelect.value;
                origSelect.value = destSelect.value;
                destSelect.value = tempSelect;
            }
            if (origLat && destLat) {
                const tempLat = origLat.value;
                origLat.value = destLat.value;
                destLat.value = tempLat;
            }
            if (origLon && destLon) {
                const tempLon = origLon.value;
                origLon.value = destLon.value;
                destLon.value = tempLon;
            }
            if (origSearch && destSearch) {
                const tempSearch = origSearch.value;
                origSearch.value = destSearch.value;
                destSearch.value = tempSearch;
            }
            showToast('Origin and Destination swapped', 'swap_vert');
        });
    }

    // =========================================================================
    // 9. Route Calculation Engine (POST /waypoints)
    // =========================================================================
    const btnCompute = document.getElementById('btn-compute');
    const spinnerCompute = document.getElementById('spinner-compute');
    const iconCompute = document.getElementById('icon-compute');
    const btnComputeText = document.getElementById('btn-compute-text');

    function setComputeLoading(isLoading) {
        if (btnCompute) btnCompute.disabled = isLoading;
        if (spinnerCompute) {
            if (isLoading) spinnerCompute.classList.remove('hidden');
            else spinnerCompute.classList.add('hidden');
        }
        if (iconCompute) {
            if (isLoading) iconCompute.classList.add('hidden');
            else iconCompute.classList.remove('hidden');
        }
        if (btnComputeText) {
            btnComputeText.textContent = isLoading ? 'Computing Optimal Route...' : 'Compute Optimal Sea Route';
        }
    }

    function computeRoute() {
        const fromLat = parseCoordinate(originLatInput ? originLatInput.value : '', true);
        const fromLon = parseCoordinate(originLonInput ? originLonInput.value : '', false);
        const toLat = parseCoordinate(destLatInput ? destLatInput.value : '', true);
        const toLon = parseCoordinate(destLonInput ? destLonInput.value : '', false);

        if (isNaN(fromLat) || isNaN(fromLon) || Math.abs(fromLat) > 90 || Math.abs(fromLon) > 180) {
            showToast('Invalid Origin coordinates. Please check inputs.', 'error');
            return;
        }

        if (isNaN(toLat) || isNaN(toLon) || Math.abs(toLat) > 90 || Math.abs(toLon) > 180) {
            showToast('Invalid Destination coordinates. Please check inputs.', 'error');
            return;
        }

        const avoidList = Array.from(document.querySelectorAll('.avoid-pill.active'))
            .map(p => p.getAttribute('data-avoid'))
            .filter(Boolean);

        const requestPayload = {
            from: { lat: fromLat, lon: fromLon },
            to: { lat: toLat, lon: toLon },
            algorithm: currentAlgorithm,
            avoid: avoidList
        };

        setComputeLoading(true);

        fetch('/waypoints', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(requestPayload)
        })
            .then(res => {
                if (!res.ok) {
                    return res.json().then(err => {
                        throw new Error(err.error || `HTTP ${res.status}`);
                    });
                }
                return res.json();
            })
            .then(data => {
                setComputeLoading(false);
                renderRoute(data);
            })
            .catch(err => {
                setComputeLoading(false);
                console.error('Route calculation error:', err);
                showToast(`Routing Failed: ${err.message}`, 'error');
            });
    }

    if (btnCompute) btnCompute.addEventListener('click', computeRoute);

    /**
     * Renders computed route onto Leaflet map & updates all metrics/manifest
     */
    function renderRoute(data) {
        if (!data || !data.features || data.features.length === 0) {
            showToast('No route found across maritime graph', 'warning');
            return;
        }

        currentRouteData = data;
        const feature = data.features[0];
        const props = feature.properties || {};
        const coords = (feature.geometry && feature.geometry.coordinates) || [];
        const waypoints = props.waypoints || [];

        currentCoords = coords;
        currentWaypoints = waypoints;

        // Clear existing route graphics
        routeLayerGroup.clearLayers();

        // Convert [lon, lat] coordinates to Leaflet [lat, lon]
        const latLngs = coords.map(c => [c[1], c[0]]);

        // 1. Double polyline: Cyan Glow + White/Cyan Flow Dash
        const isDark = document.documentElement.classList.contains('dark');
        currentGlowPolyline = L.polyline(latLngs, {
            color: isDark ? '#0ea5e9' : '#0284c7',
            weight: 6,
            opacity: 0.45,
            lineCap: 'round',
            lineJoin: 'round'
        });

        currentFlowPolyline = L.polyline(latLngs, {
            color: isDark ? '#e0f2fe' : '#0369a1',
            weight: 2.5,
            opacity: 0.95,
            className: 'flow-dash'
        });

        routeLayerGroup.addLayer(currentGlowPolyline);
        routeLayerGroup.addLayer(currentFlowPolyline);

        // 2. Custom Waypoint Markers
        if (latLngs.length > 0) {
            // Origin Marker
            const originIcon = L.divIcon({
                className: 'custom-wp-origin',
                html: '<div class="w-4 h-4 rounded-full bg-primary border-2 border-[#070e1b] shadow-lg flex items-center justify-center"><div class="w-1.5 h-1.5 rounded-full bg-[#070e1b]"></div></div>',
                iconSize: [16, 16],
                iconAnchor: [8, 8]
            });
            const originMarker = L.marker(latLngs[0], { icon: originIcon }).bindPopup(
                `<b>Origin Departure</b><br>${formatCoordDMS(latLngs[0][0], true)}, ${formatCoordDMS(latLngs[0][1], false)}`
            );
            routeLayerGroup.addLayer(originMarker);

            // Destination Marker
            const destIcon = L.divIcon({
                className: 'custom-wp-dest',
                html: '<div class="relative flex items-center justify-center" style="width:20px;height:20px;"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-tertiary opacity-75"></span><span class="relative inline-flex rounded-full h-3.5 w-3.5 bg-tertiary border-2 border-[#070e1b]"></span></div>',
                iconSize: [20, 20],
                iconAnchor: [10, 10]
            });
            const destMarker = L.marker(latLngs[latLngs.length - 1], { icon: destIcon }).bindPopup(
                `<b>Destination Arrival</b><br>${formatCoordDMS(latLngs[latLngs.length - 1][0], true)}, ${formatCoordDMS(latLngs[latLngs.length - 1][1], false)}`
            );
            routeLayerGroup.addLayer(destMarker);

            // Intermediate Waypoint markers with passage annotations
            waypoints.forEach((wp, idx) => {
                if (idx === 0 || idx === waypoints.length - 1) return;
                const isPassage = Boolean(wp.passage);
                const isSampled = waypoints.length <= 20 || idx % Math.max(1, Math.floor(waypoints.length / 12)) === 0;

                if (isPassage || isSampled) {
                    const color = isPassage ? '#f59e0b' : '#4cd7f6';
                    const size = isPassage ? 10 : 7;
                    const wpIcon = L.divIcon({
                        className: 'custom-wp-dot',
                        html: `<div style="width:${size}px;height:${size}px;border-radius:50%;background-color:${color};border:1.5px solid #070e1b;box-shadow:0 1px 3px rgba(0,0,0,0.5);"></div>`,
                        iconSize: [size, size],
                        iconAnchor: [size / 2, size / 2]
                    });
                    const marker = L.marker([wp.coordinates[1], wp.coordinates[0]], { icon: wpIcon });
                    marker.bindPopup(
                        `<b>#${wp.index} ${wp.name}</b>${wp.passage ? '<br><span style="color:#f59e0b;font-weight:bold;">Passage: ' + wp.passage + '</span>' : ''}<br>Leg: ${wp.leg_distance_nm.toFixed(1)} NM<br>Cumul: ${wp.cum_distance_nm.toFixed(1)} NM<br>${formatCoordDMS(wp.coordinates[1], true)}, ${formatCoordDMS(wp.coordinates[0], false)}`
                    );
                    routeLayerGroup.addLayer(marker);
                }
            });
        }

        // Fit map bounds to show route
        if (currentGlowPolyline) {
            map.fitBounds(currentGlowPolyline.getBounds(), { padding: [50, 50] });
        }

        // 3. Update Metrics Summary
        const totalDistNM = props.total_dist_nm || (props.total_dist / 1.852);
        const totalDistKM = props.total_dist || (totalDistNM * 1.852);
        const totalDistMI = props.total_dist_mi || (totalDistKM * 0.621371);
        const nodeCount = props.node_count || coords.length;

        currentTotalDistNM = totalDistNM;

        const statDist = document.getElementById('stat-dist');
        const statDistAlt = document.getElementById('stat-dist-alt');
        const statNodes = document.getElementById('stat-nodes');
        const statStatus = document.getElementById('stat-status');
        const collapsedDistVal = document.getElementById('collapsed-dist-val');
        const collapsedPtsVal = document.getElementById('collapsed-pts-val');

        if (statDist) statDist.innerHTML = `${Math.round(totalDistNM).toLocaleString()} <span class="text-[10px] font-normal">NM</span>`;
        if (statDistAlt) statDistAlt.textContent = `${Math.round(totalDistKM).toLocaleString()} KM • ${Math.round(totalDistMI).toLocaleString()} MI`;
        if (statNodes) statNodes.innerHTML = `${nodeCount} <span class="text-[10px] font-normal">Nodes</span>`;
        if (statStatus) statStatus.textContent = 'Optimal Solved';

        if (collapsedDistVal) {
            collapsedDistVal.textContent = totalDistNM >= 1000 ? `${(totalDistNM / 1000).toFixed(1)}k` : Math.round(totalDistNM);
        }
        if (collapsedPtsVal) {
            collapsedPtsVal.textContent = nodeCount;
        }

        // Top Navigation Header Badges
        const headerDistBadge = document.getElementById('header-dist-badge');
        const headerOriginBadge = document.getElementById('header-origin-badge');
        const headerDestBadge = document.getElementById('header-dest-badge');

        if (headerDistBadge) headerDistBadge.textContent = `${Math.round(totalDistNM).toLocaleString()} NM`;
        if (headerOriginBadge) {
            const originLabel = props.route_name ? props.route_name.split('->')[0].trim() : 'Origin';
            headerOriginBadge.innerHTML = `<span class="w-1.5 h-1.5 rounded-none bg-primary animate-pulse"></span> ${originLabel}`;
        }
        if (headerDestBadge) {
            const destLabel = props.route_name ? props.route_name.split('->')[1].trim() : 'Destination';
            headerDestBadge.textContent = destLabel;
        }

        // Transit duration update based on current speed slider
        updateTransitDuration();

        // 4. Update Waypoint Manifest Table
        renderManifestTable(waypoints);

        showToast(`Sea route calculated successfully (${Math.round(totalDistNM).toLocaleString()} NM)`);
    }

    /**
     * Renders tabular waypoint manifest
     */
    function renderManifestTable(waypoints) {
        const tbody = document.getElementById('waypoint-rows');
        const manifestHeader = document.getElementById('manifest-header');
        if (!tbody) return;

        if (manifestHeader) {
            manifestHeader.textContent = `Waypoint Manifest (${waypoints.length} Points)`;
        }

        if (!waypoints || waypoints.length === 0) {
            tbody.innerHTML = '<tr class="text-on-surface-variant text-center"><td colspan="4" class="py-3 px-2 text-[10px] text-outline font-mono">No route calculated.</td></tr>';
            return;
        }

        tbody.innerHTML = '';
        waypoints.forEach(wp => {
            const tr = document.createElement('tr');
            tr.className = 'hover:bg-surface-container/60 transition-colors border-b border-surface-container-high/40 text-[10px]';
            tr.innerHTML = `
                <td class="py-1 px-2 text-outline-variant font-semibold">#${wp.index}</td>
                <td class="py-1 px-2">
                    <div class="flex flex-col">
                        <span class="font-medium text-on-surface flex items-center gap-1">
                            ${wp.name}
                            ${wp.passage ? '<span class="text-[8px] px-1 py-0.2 rounded-sm bg-warning/20 text-warning uppercase font-semibold">' + wp.passage + '</span>' : ''}
                        </span>
                        <span class="text-[9px] text-on-surface-variant font-mono">
                            ${formatCoordDMS(wp.coordinates[1], true)}, ${formatCoordDMS(wp.coordinates[0], false)}
                        </span>
                    </div>
                </td>
                <td class="py-1 px-2 text-right text-secondary font-mono">${(wp.leg_distance_nm || 0).toFixed(1)} NM</td>
                <td class="py-1 px-2 text-right text-primary font-bold font-mono">${(wp.cum_distance_nm || 0).toFixed(1)} NM</td>
            `;
            tbody.appendChild(tr);
        });
    }

    // =========================================================================
    // 10. Transit Speed Slider & Voyage Duration
    // =========================================================================
    const speedSlider = document.getElementById('speed-slider');
    const speedLabel = document.getElementById('speed-label');
    const statTime = document.getElementById('stat-time');
    const headerDurationBadge = document.getElementById('header-duration-badge');

    function updateTransitDuration() {
        if (!speedSlider) return;
        const speed = parseFloat(speedSlider.value) || 18.0;
        if (speedLabel) speedLabel.textContent = `${speed.toFixed(1)} kts`;

        const dist = currentTotalDistNM || 8420;
        const totalHours = dist / speed;
        const days = Math.floor(totalHours / 24);
        const hours = Math.floor(totalHours % 24);
        const mins = Math.round((totalHours - Math.floor(totalHours)) * 60);
        const durationStr = `${days}d ${hours}h ${mins}m`;

        if (statTime) statTime.textContent = durationStr;
        if (headerDurationBadge) headerDurationBadge.textContent = `${days}d ${hours}h @ ${speed.toFixed(1)} kts`;

        const simTelemetry = document.getElementById('sim-vessel-telemetry');
        if (simTelemetry) {
            if (simActive && simProgress > 0) {
                const remDist = Math.max(0, dist * (1 - simProgress / 100));
                const remHours = remDist / speed;
                const remDays = Math.floor(remHours / 24);
                const remHrs = Math.floor(remHours % 24);
                simTelemetry.textContent = `ETA: ${remDays}d ${remHrs}h • ${speed.toFixed(1)} kts`;
            } else {
                simTelemetry.textContent = `ETA: ${days}d ${hours}h • ${speed.toFixed(1)} kts`;
            }
        }

        if (simActive) {
            updateSimulationUI();
        }
    }

    if (speedSlider) {
        speedSlider.addEventListener('input', updateTransitDuration);
    }

    // =========================================================================
    // 11. Route Simulation / Playback Engine
    // =========================================================================
    const btnStartSim = document.getElementById('btn-start-simulation');
    const btnQuickSimTrigger = document.getElementById('btn-quick-sim-trigger');
    const simPlayerDock = document.getElementById('simulation-player-dock');
    const btnStopSim = document.getElementById('btn-stop-sim');
    const btnPlayPauseSim = document.getElementById('btn-play-pause-sim');
    const simPlayIcon = document.getElementById('sim-play-icon');
    const btnRestartSim = document.getElementById('btn-restart-sim');
    const simScrubber = document.getElementById('sim-scrubber');
    const simProgressDist = document.getElementById('sim-progress-dist');
    const simProgressPct = document.getElementById('sim-progress-pct');
    const simElapsed = document.getElementById('sim-elapsed');
    const simLegName = document.getElementById('sim-leg-name');
    const simCurrentPos = document.getElementById('sim-current-pos');
    const simSpeedBtns = document.querySelectorAll('.sim-speed-btn');

    // Default Rotterdam -> Singapore route waypoints when simulation is invoked before calculation
    const FALLBACK_SIM_WAYPOINTS = [
        { name: "Rotterdam (Departure)", lat: 51.95, lon: 4.05 },
        { name: "English Channel TSS", lat: 50.12, lon: -0.45 },
        { name: "Strait of Gibraltar", lat: 35.95, lon: -5.60 },
        { name: "Suez Canal Approach", lat: 31.26, lon: 32.31 },
        { name: "Bab-el-Mandeb Strait", lat: 12.58, lon: 43.33 },
        { name: "Malacca Strait TSS", lat: 2.82, lon: 101.21 },
        { name: "Singapore Roads", lat: 1.26, lon: 103.82 }
    ];

    function getSimulationCoordinates() {
        if (currentCoords && currentCoords.length > 1) {
            return currentCoords.map(c => [c[1], c[0]]); // [lat, lon]
        }
        return FALLBACK_SIM_WAYPOINTS.map(wp => [wp.lat, wp.lon]);
    }

    /**
     * Computes spherical forward azimuth / heading angle (in degrees 0-360)
     * between consecutive points p0 and p1:
     * Math.atan2(Math.sin(dLon)*Math.cos(lat1), Math.cos(lat0)*Math.sin(lat1) - Math.sin(lat0)*Math.cos(lat1)*Math.cos(dLon))
     */
    function computeHeading(lat0, lon0, lat1, lon1) {
        if (Math.abs(lat0 - lat1) < 1e-6 && Math.abs(lon0 - lon1) < 1e-6) {
            return 0;
        }
        const toRad = deg => deg * Math.PI / 180;
        const phi0 = toRad(lat0);
        const phi1 = toRad(lat1);
        const dLon = toRad(lon1 - lon0);

        const y = Math.sin(dLon) * Math.cos(phi1);
        const x = Math.cos(phi0) * Math.sin(phi1) - Math.sin(phi0) * Math.cos(phi1) * Math.cos(dLon);
        let heading = Math.atan2(y, x) * 180 / Math.PI;
        heading = (heading + 360) % 360;
        return heading;
    }

    function getWaypointName(index, total) {
        if (currentWaypoints && currentWaypoints.length > 0) {
            const wp = currentWaypoints[index];
            if (index === 0) {
                if (originPreset && PRESET_COORDS[originPreset.value]) {
                    return PRESET_COORDS[originPreset.value].name.split(',')[0].trim();
                }
                const originSearch = document.getElementById('origin-search');
                if (originSearch && originSearch.value) {
                    return originSearch.value.split(',')[0].trim();
                }
                if (currentRouteData && currentRouteData.features && currentRouteData.features[0].properties && currentRouteData.features[0].properties.route_name) {
                    const rName = currentRouteData.features[0].properties.route_name.split(' -> ')[0];
                    if (rName && rName !== 'Origin') return rName.trim();
                }
                return (wp && wp.name) || 'Origin';
            }
            if (index === total - 1) {
                if (destPreset && PRESET_COORDS[destPreset.value]) {
                    return PRESET_COORDS[destPreset.value].name.split(',')[0].trim();
                }
                const destSearch = document.getElementById('dest-search');
                if (destSearch && destSearch.value) {
                    return destSearch.value.split(',')[0].trim();
                }
                if (currentRouteData && currentRouteData.features && currentRouteData.features[0].properties && currentRouteData.features[0].properties.route_name) {
                    const rName = currentRouteData.features[0].properties.route_name.split(' -> ')[1];
                    if (rName && rName !== 'Destination') return rName.trim();
                }
                return (wp && wp.name) || 'Destination';
            }
            if (wp) {
                if (wp.passage && PASSAGE_LABELS[wp.passage]) {
                    return PASSAGE_LABELS[wp.passage];
                }
                return wp.name || `Waypoint ${index + 1}`;
            }
            return `Waypoint ${index + 1}`;
        }
        if (FALLBACK_SIM_WAYPOINTS[index]) {
            return FALLBACK_SIM_WAYPOINTS[index].name;
        }
        return `Waypoint ${index + 1}`;
    }

    function createVesselMarker(initialLatLng) {
        if (simVesselMarker) {
            map.removeLayer(simVesselMarker);
        }
        const speed = (speedSlider ? parseFloat(speedSlider.value) : 18.0) || 18.0;
        const coords = getSimulationCoordinates();
        let initialHeading = 0;
        if (coords.length >= 2) {
            initialHeading = computeHeading(coords[0][0], coords[0][1], coords[1][0], coords[1][1]);
        }
        const vesselIcon = L.divIcon({
            className: 'vessel-marker-container',
            html: `
                <div class="relative pointer-events-none select-none" style="width:0;height:0;">
                    <div class="vessel-beacon absolute rounded-full bg-tertiary" style="width:32px;height:32px;left:-16px;top:-16px;opacity:0.75;"></div>
                    <div class="relative w-6 h-6 -left-3 -top-3 rounded-full bg-surface-container-lowest border-[1.5px] border-tertiary flex items-center justify-center shadow-lg">
                        <span id="sim-vessel-nav-icon" class="sim-nav-icon material-symbols-outlined text-tertiary text-[14px] font-bold inline-block" style="transform: rotate(${Math.round(initialHeading)}deg); transform-origin: center; transition: transform 0.12s linear;">navigation</span>
                    </div>
                    <div class="absolute left-4 -top-2.5 bg-surface-container-lowest/95 border border-tertiary/80 rounded-sm px-1.5 py-0.5 shadow-xl whitespace-nowrap flex items-center gap-1 font-mono leading-tight text-on-surface">
                        <span class="w-1.5 h-1.5 rounded-full bg-tertiary animate-pulse inline-block"></span>
                        <span id="sim-marker-speed" class="sim-marker-speed text-on-surface text-[8.5px] font-semibold">${speed.toFixed(1)} kts • HDG ${Math.round(initialHeading).toString().padStart(3, '0')}°</span>
                    </div>
                </div>
            `,
            iconSize: [0, 0],
            iconAnchor: [0, 0]
        });
        simVesselMarker = L.marker(initialLatLng, { icon: vesselIcon, zIndexOffset: 1000 }).addTo(map);
    }

    function openSimulation() {
        simActive = true;
        if (simPlayerDock) simPlayerDock.classList.remove('hidden');
        const coords = getSimulationCoordinates();
        createVesselMarker(coords[0]);
        updateSimulationUI();
        playSimulation();
        showToast('Route Simulation Mode Activated', 'play_circle');
    }

    function stopSimulation() {
        simActive = false;
        pauseSimulation();
        simProgress = 0;
        if (simScrubber) simScrubber.value = 0;
        if (simPlayerDock) simPlayerDock.classList.add('hidden');
        if (simVesselMarker) {
            map.removeLayer(simVesselMarker);
            simVesselMarker = null;
        }
        if (simLegName) {
            simLegName.textContent = '• Inactive';
        }
        updateTransitDuration();
        showToast('Simulation Stopped', 'stop');
    }

    function playSimulation() {
        simPlaying = true;
        if (simPlayIcon) simPlayIcon.textContent = 'pause';
        clearInterval(simInterval);
        simInterval = setInterval(() => {
            simProgress += 0.25 * simSpeedMultiplier;
            if (simProgress >= 100) {
                simProgress = 100;
                pauseSimulation();
                showToast('Destination Reached: Voyage Complete', 'task_alt');
            }
            if (simScrubber) simScrubber.value = simProgress;
            updateSimulationUI();
        }, 50);
    }

    function pauseSimulation() {
        simPlaying = false;
        if (simPlayIcon) simPlayIcon.textContent = 'play_arrow';
        clearInterval(simInterval);
    }

    function updateSimulationUI() {
        const totalDist = currentTotalDistNM || 8420;
        const currentDist = Math.round((simProgress / 100) * totalDist);
        const speed = (speedSlider ? parseFloat(speedSlider.value) : 18.0) || 18.0;
        const totalDays = totalDist / (speed * 24);
        const currentDays = ((simProgress / 100) * totalDays).toFixed(1);

        if (simProgressDist) simProgressDist.textContent = `${currentDist.toLocaleString()} NM / ${Math.round(totalDist).toLocaleString()} NM`;
        if (simProgressPct) simProgressPct.textContent = `${Math.round(simProgress)}% Complete`;
        if (simElapsed) simElapsed.textContent = `Day ${currentDays}`;

        // Live ETA and speed telemetry
        const simTelemetry = document.getElementById('sim-vessel-telemetry');
        if (simTelemetry) {
            const remainingDist = Math.max(0, totalDist * (1 - simProgress / 100));
            const remainingHours = remainingDist / speed;
            if (simProgress >= 100) {
                simTelemetry.textContent = `Arrived • ${speed.toFixed(1)} kts`;
            } else {
                const remDays = Math.floor(remainingHours / 24);
                const remHrs = Math.floor(remainingHours % 24);
                simTelemetry.textContent = `ETA: ${remDays}d ${remHrs}h • ${speed.toFixed(1)} kts`;
            }
        }

        updateVesselPosition(simProgress / 100);
    }

    function updateVesselPosition(ratio) {
        const coords = getSimulationCoordinates();
        if (coords.length < 2) return;

        const numSegments = coords.length - 1;
        const exactIndex = ratio * numSegments;
        const segmentIndex = Math.min(Math.floor(exactIndex), numSegments - 1);
        const t = exactIndex - segmentIndex;

        const p0 = coords[segmentIndex];
        const p1 = coords[segmentIndex + 1];

        const lat = p0[0] + (p1[0] - p0[0]) * t;
        const lon = p0[1] + (p1[1] - p0[1]) * t;

        const heading = computeHeading(p0[0], p0[1], p1[0], p1[1]);
        const speed = (speedSlider ? parseFloat(speedSlider.value) : 18.0) || 18.0;

        if (simVesselMarker) {
            simVesselMarker.setLatLng([lat, lon]);
            const markerEl = simVesselMarker.getElement();
            if (markerEl) {
                const navIcon = markerEl.querySelector('#sim-vessel-nav-icon') || markerEl.querySelector('.sim-nav-icon');
                if (navIcon) {
                    navIcon.style.transform = `rotate(${Math.round(heading)}deg)`;
                }
                const markerSpeed = markerEl.querySelector('#sim-marker-speed') || markerEl.querySelector('.sim-marker-speed');
                if (markerSpeed) {
                    markerSpeed.textContent = `${speed.toFixed(1)} kts • HDG ${Math.round(heading).toString().padStart(3, '0')}°`;
                }
            }
        }

        // Leg Description
        if (simLegName) {
            const fromName = getWaypointName(segmentIndex, coords.length);
            const toName = getWaypointName(segmentIndex + 1, coords.length);
            if (fromName && toName) {
                simLegName.textContent = `• Leg ${segmentIndex + 1}/${numSegments}: ${fromName} ➔ ${toName}`;
            } else {
                simLegName.textContent = `• Leg ${segmentIndex + 1}/${numSegments}`;
            }
        }

        if (simCurrentPos) {
            simCurrentPos.textContent = `Position: ${lat.toFixed(2)}°, ${lon.toFixed(2)}°`;
        }
    }

    if (btnStartSim) btnStartSim.addEventListener('click', openSimulation);
    if (btnQuickSimTrigger) btnQuickSimTrigger.addEventListener('click', openSimulation);
    if (btnStopSim) btnStopSim.addEventListener('click', stopSimulation);

    if (btnPlayPauseSim) {
        btnPlayPauseSim.addEventListener('click', () => {
            if (simPlaying) pauseSimulation();
            else {
                if (simProgress >= 100) simProgress = 0;
                playSimulation();
            }
        });
    }

    if (btnRestartSim) {
        btnRestartSim.addEventListener('click', () => {
            simProgress = 0;
            if (simScrubber) simScrubber.value = 0;
            updateSimulationUI();
            if (!simPlaying) playSimulation();
        });
    }

    if (simScrubber) {
        simScrubber.addEventListener('input', (e) => {
            simProgress = parseFloat(e.target.value);
            updateSimulationUI();
        });
    }

    simSpeedBtns.forEach(btn => {
        btn.addEventListener('click', () => {
            simSpeedBtns.forEach(b => {
                b.className = 'sim-speed-btn px-2 py-0.5 rounded-sm text-on-surface-variant hover:text-on-surface cursor-pointer';
            });
            btn.className = 'sim-speed-btn active px-2 py-0.5 rounded-sm bg-primary-container text-on-primary-container font-semibold cursor-pointer';
            simSpeedMultiplier = parseFloat(btn.dataset.speed) || 1;
        });
    });

    // =========================================================================
    // 12. Exports & Sharing (CSV, GeoJSON, Permalinks)
    // =========================================================================
    const btnExportCSV = document.getElementById('btn-export-csv');
    const btnDownloadGeoJSON = document.getElementById('btn-download-geojson');
    const btnCopyGeoJSON = document.getElementById('btn-copy-geojson');
    const btnShareRoute = document.getElementById('btn-share-route');

    // 12.1 CSV Export
    if (btnExportCSV) {
        btnExportCSV.addEventListener('click', () => {
            if (!currentWaypoints || currentWaypoints.length === 0) {
                showToast('No route waypoints calculated to export', 'warning');
                return;
            }

            let csvContent = 'Leg,Waypoint,Latitude,Longitude,Leg_Distance_NM,Cumulative_Distance_NM,Passage\r\n';
            currentWaypoints.forEach(wp => {
                const name = (wp.name || '').replace(/,/g, ' ');
                const passage = wp.passage || '';
                csvContent += `${wp.index},"${name}",${wp.coordinates[1].toFixed(5)},${wp.coordinates[0].toFixed(5)},${(wp.leg_distance_nm || 0).toFixed(2)},${(wp.cum_distance_nm || 0).toFixed(2)},"${passage}"\r\n`;
            });

            const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.setAttribute('href', url);
            a.setAttribute('download', 'seaRoute_manifest.csv');
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
            showToast(`Exported ${currentWaypoints.length} waypoints as CSV`);
        });
    }

    // 12.2 GeoJSON Download
    if (btnDownloadGeoJSON) {
        btnDownloadGeoJSON.addEventListener('click', () => {
            if (!currentRouteData) {
                showToast('No calculated route to download', 'warning');
                return;
            }
            const jsonStr = JSON.stringify(currentRouteData, null, 2);
            const blob = new Blob([jsonStr], { type: 'application/geo+json;charset=utf-8;' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.setAttribute('href', url);
            a.setAttribute('download', 'seaRoute_manifest.geojson');
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
            showToast('Downloaded seaRoute_manifest.geojson');
        });
    }

    // 12.3 GeoJSON Copy
    if (btnCopyGeoJSON) {
        btnCopyGeoJSON.addEventListener('click', () => {
            if (!currentRouteData) {
                showToast('No calculated route to copy', 'warning');
                return;
            }
            const jsonStr = JSON.stringify(currentRouteData, null, 2);
            navigator.clipboard.writeText(jsonStr)
                .then(() => showToast('GeoJSON copied to clipboard'))
                .catch(() => showToast('Failed to copy GeoJSON to clipboard', 'error'));
        });
    }

    // 12.4 Share Permalink
    if (btnShareRoute) {
        btnShareRoute.addEventListener('click', () => {
            const fromLat = parseCoordinate(originLatInput ? originLatInput.value : '', true);
            const fromLon = parseCoordinate(originLonInput ? originLonInput.value : '', false);
            const toLat = parseCoordinate(destLatInput ? destLatInput.value : '', true);
            const toLon = parseCoordinate(destLonInput ? destLonInput.value : '', false);
            const avoidList = Array.from(document.querySelectorAll('.avoid-pill.active'))
                .map(p => p.getAttribute('data-avoid'))
                .filter(Boolean);
            const speed = speedSlider ? speedSlider.value : '18';

            const params = new URLSearchParams();
            if (!isNaN(fromLat) && !isNaN(fromLon)) {
                params.set('from', `${fromLat.toFixed(4)},${fromLon.toFixed(4)}`);
                params.set('from_lat', fromLat.toFixed(4));
                params.set('from_lon', fromLon.toFixed(4));
            }
            if (!isNaN(toLat) && !isNaN(toLon)) {
                params.set('to', `${toLat.toFixed(4)},${toLon.toFixed(4)}`);
                params.set('to_lat', toLat.toFixed(4));
                params.set('to_lon', toLon.toFixed(4));
            }
            if (currentAlgorithm) params.set('algo', currentAlgorithm);
            if (avoidList.length > 0) params.set('avoid', avoidList.join(','));
            if (speed) params.set('speed', speed);

            const shareUrl = `${window.location.origin}${window.location.pathname}?${params.toString()}`;
            window.history.replaceState({}, '', shareUrl);

            navigator.clipboard.writeText(shareUrl)
                .then(() => showToast('Route permalink copied to clipboard', 'link'))
                .catch(() => showToast('Permalink updated in address bar', 'link'));
        });
    }

    // =========================================================================
    // 13. URL Rehydration & Initial Computation
    // =========================================================================
    function rehydrateFromURL() {
        const params = new URLSearchParams(window.location.search);
        let hasCustomParams = false;

        const setOrigin = (lat, lon, preset, name) => {
            if (originLatInput) originLatInput.value = formatCoordDMS(lat, true);
            if (originLonInput) originLonInput.value = formatCoordDMS(lon, false);
            if (originPreset) originPreset.value = preset || 'CUSTOM';
            const originSearch = document.getElementById('origin-search');
            if (originSearch && name) originSearch.value = name;
        };

        const setDest = (lat, lon, preset, name) => {
            if (destLatInput) destLatInput.value = formatCoordDMS(lat, true);
            if (destLonInput) destLonInput.value = formatCoordDMS(lon, false);
            if (destPreset) destPreset.value = preset || 'CUSTOM';
            const destSearch = document.getElementById('dest-search');
            if (destSearch && name) destSearch.value = name;
        };

        let originPending = null;
        let destPending = null;

        if (params.has('from')) {
            const fromVal = params.get('from');
            const loc = resolveLocationParam(fromVal);
            if (loc) {
                setOrigin(loc.lat, loc.lon, loc.preset, loc.name);
                hasCustomParams = true;
            } else {
                originPending = fetch(`/ports?search=${encodeURIComponent(fromVal)}`)
                    .then(res => res.ok ? res.json() : [])
                    .then(ports => {
                        if (ports && ports.length > 0) {
                            const p = ports[0];
                            setOrigin(p.latitude, p.longitude, 'CUSTOM', `${p.port}, ${p.country}`);
                            hasCustomParams = true;
                        }
                    }).catch(() => {});
            }
        } else if (params.has('from_lat') && params.has('from_lon')) {
            const lat = parseFloat(params.get('from_lat'));
            const lon = parseFloat(params.get('from_lon'));
            if (!isNaN(lat) && !isNaN(lon)) {
                setOrigin(lat, lon, 'CUSTOM', `${lat.toFixed(2)}, ${lon.toFixed(2)}`);
                hasCustomParams = true;
            }
        }

        if (params.has('to')) {
            const toVal = params.get('to');
            const loc = resolveLocationParam(toVal);
            if (loc) {
                setDest(loc.lat, loc.lon, loc.preset, loc.name);
                hasCustomParams = true;
            } else {
                destPending = fetch(`/ports?search=${encodeURIComponent(toVal)}`)
                    .then(res => res.ok ? res.json() : [])
                    .then(ports => {
                        if (ports && ports.length > 0) {
                            const p = ports[0];
                            setDest(p.latitude, p.longitude, 'CUSTOM', `${p.port}, ${p.country}`);
                            hasCustomParams = true;
                        }
                    }).catch(() => {});
            }
        } else if (params.has('to_lat') && params.has('to_lon')) {
            const lat = parseFloat(params.get('to_lat'));
            const lon = parseFloat(params.get('to_lon'));
            if (!isNaN(lat) && !isNaN(lon)) {
                setDest(lat, lon, 'CUSTOM', `${lat.toFixed(2)}, ${lon.toFixed(2)}`);
                hasCustomParams = true;
            }
        }

        if (params.has('algo')) {
            setAlgorithm(params.get('algo'));
        }

        if (params.has('avoid')) {
            const avoids = params.get('avoid').split(',').map(s => s.trim().toLowerCase());
            document.querySelectorAll('.avoid-pill').forEach(pill => {
                const code = (pill.getAttribute('data-avoid') || '').toLowerCase();
                if (avoids.includes(code)) {
                    pill.classList.add('active');
                    pill.className = 'avoid-pill active px-2 py-1 rounded-sm text-[10px] font-mono flex items-center gap-1 transition-all border bg-warning/20 border-warning text-warning shadow-sm font-semibold cursor-pointer';
                    const icon = pill.querySelector('.material-symbols-outlined');
                    if (icon) {
                        icon.textContent = 'do_not_disturb_on';
                        icon.className = 'material-symbols-outlined text-[12px]';
                    }
                }
            });
            updateAvoidBadge();
        }

        if (params.has('speed')) {
            const sp = parseFloat(params.get('speed'));
            if (!isNaN(sp) && speedSlider) {
                speedSlider.value = sp;
                updateTransitDuration();
            }
        }

        // Trigger initial calculation (default or hydrated parameters)
        if (originPending || destPending) {
            Promise.all([originPending, destPending]).then(() => computeRoute());
        } else {
            computeRoute();
        }
    }

    // =========================================================================
    // 14. Theme & Layout Shell Interactions
    // =========================================================================

    // 14.1 Dark / Light Theme Toggle
    const themeBtn = document.getElementById('theme-toggle-btn');
    const themeIcon = document.getElementById('theme-icon');
    const themeText = document.getElementById('theme-text');

    function updateRouteTheme(isDark) {
        if (currentGlowPolyline) {
            currentGlowPolyline.setStyle({ color: isDark ? '#0ea5e9' : '#0284c7' });
        }
        if (currentFlowPolyline) {
            currentFlowPolyline.setStyle({ color: isDark ? '#e0f2fe' : '#0369a1' });
        }
    }

    function applyTheme(isDark) {
        if (isDark) {
            document.documentElement.classList.add('dark');
        } else {
            document.documentElement.classList.remove('dark');
        }
        if (themeIcon) themeIcon.textContent = isDark ? 'light_mode' : 'dark_mode';
        if (themeText) themeText.textContent = isDark ? 'Light' : 'Dark';

        // Auto-switch between Carto Dark and Carto Light
        if (currentBaseLayerKey === 'carto-dark' && !isDark) {
            switchBaseLayer('carto-light');
        } else if (currentBaseLayerKey === 'carto-light' && isDark) {
            switchBaseLayer('carto-dark');
        }

        updateRouteTheme(isDark);
        try {
            localStorage.setItem('searoute_theme', isDark ? 'dark' : 'light');
        } catch (e) {}
    }

    if (themeBtn) {
        themeBtn.addEventListener('click', () => {
            const isCurrentlyDark = document.documentElement.classList.contains('dark');
            const willBeDark = !isCurrentlyDark;
            applyTheme(willBeDark);
            showToast(willBeDark ? 'Tactical Dark theme activated' : 'Maritime Light theme activated', 'palette');
        });
    }

    // Initialize saved theme preference
    try {
        const savedTheme = localStorage.getItem('searoute_theme');
        if (savedTheme === 'light') {
            applyTheme(false);
        }
    } catch (e) {}

    // 14.2 Desktop Sidebar Collapse & Expand
    const mainSidebar = document.getElementById('main-sidebar');
    const collapsedSidebar = document.getElementById('collapsed-sidebar');
    const btnCollapse = document.getElementById('btn-collapse-sidebar');
    const btnExpand = document.getElementById('btn-expand-sidebar');
    const mobileSidebarToggle = document.getElementById('mobile-sidebar-toggle');
    const mobileSidebarClose = document.getElementById('mobile-sidebar-close');

    function collapseSidebar() {
        if (mainSidebar && collapsedSidebar) {
            mainSidebar.classList.add('lg:hidden', '-translate-x-full');
            collapsedSidebar.classList.remove('hidden');
            collapsedSidebar.classList.add('flex');
            setTimeout(() => map.invalidateSize(), 320);
        }
    }

    function expandSidebar() {
        if (mainSidebar && collapsedSidebar) {
            mainSidebar.classList.remove('lg:hidden', '-translate-x-full');
            collapsedSidebar.classList.add('hidden');
            collapsedSidebar.classList.remove('flex');
            setTimeout(() => map.invalidateSize(), 320);
        }
    }

    if (btnCollapse) btnCollapse.addEventListener('click', collapseSidebar);
    if (btnExpand) btnExpand.addEventListener('click', expandSidebar);

    if (mobileSidebarToggle && mainSidebar) {
        mobileSidebarToggle.addEventListener('click', () => {
            mainSidebar.classList.toggle('-translate-x-full');
        });
    }

    if (mobileSidebarClose && mainSidebar) {
        mobileSidebarClose.addEventListener('click', () => {
            mainSidebar.classList.add('-translate-x-full');
        });
    }

    // Ctrl+B / Cmd+B keyboard shortcut for sidebar toggle
    document.addEventListener('keydown', (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'b') {
            e.preventDefault();
            if (mainSidebar) {
                if (mainSidebar.classList.contains('lg:hidden')) expandSidebar();
                else collapseSidebar();
            }
        }
    });

    // 14.3 Map Layers Panel Collapse & Expand
    const layerCard = document.getElementById('layer-panel-card');
    const collapseLayerBtn = document.getElementById('btn-collapse-layer-panel');
    const expandLayerBtn = document.getElementById('btn-expand-layer-panel');

    if (collapseLayerBtn && expandLayerBtn) {
        collapseLayerBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (layerCard) layerCard.classList.add('hidden');
            expandLayerBtn.classList.remove('hidden');
            expandLayerBtn.classList.add('flex');
            showToast('Map layers panel minimized');
        });

        expandLayerBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (layerCard) layerCard.classList.remove('hidden');
            expandLayerBtn.classList.add('hidden');
            expandLayerBtn.classList.remove('flex');
        });
    }

    // 14.4 Window resize recalculation
    window.addEventListener('resize', () => {
        if (map) map.invalidateSize();
    });

    // Run rehydration and initial computation on startup
    rehydrateFromURL();

})();
