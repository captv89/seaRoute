// App Configuration
const OWM_API_KEY = (window.__APP_CONFIG__ && window.__APP_CONFIG__.owmApiKey) || '';
// CARTO basemaps need a key since 2026; without one their layers are hidden
const CARTO_API_KEY = (window.__APP_CONFIG__ && window.__APP_CONFIG__.cartoApiKey) || '';

// Create a map and add it to the page
const spinOps = {
    lines: 13,
    length: 38,
    width: 17,
    radius: 45,
    scale: 1,
    corners: 1,
    speed: 1,
    rotate: 0,
    animation: 'spinner-line-fade-quick',
    direction: 1,
    color: '#ffffff',
    fadeColor: 'transparent',
    top: '50%',
    left: '50%',
    shadow: '0 0 1px transparent',
    zIndex: 2000000000,
    className: 'spinner',
    position: 'absolute',
};

const spinElement = document.getElementById('map');
const spinner = new Spinner(spinOps).spin(spinElement);
spinner.stop();

// Clean Coordinates
function CleaCoordinates(lat, lng) {
    let absLat = Math.abs(lat);
    let absLng = Math.abs(lng);

    const latDeg = Math.floor(absLat);
    const latDegString = latDeg.toString().padStart(2, '0');
    const latMin = ((absLat - latDeg) * 60).toFixed(2);
    const latDir = lat >= 0 ? 'N' : 'S';

    const lngDeg = Math.floor(absLng);
    const lngDegString = lngDeg.toString().padStart(3, '0');
    const lngMin = ((absLng - lngDeg) * 60).toFixed(2);
    const lngDir = lng >= 0 ? 'E' : 'W';

    return `${latDegString}\u00b0 ${latMin}' ${latDir}, ${lngDegString}\u00b0 ${lngMin}' ${lngDir}`;
}

const map = L.map("map", {
    preferCanvas: true,
}).setView([0, 0], 2);

// OpenStreetMap is the fallback base map when no CARTO key is set
let osm = L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    maxZoom: 19,
    className: 'map-tiles'
});

let openSeaMapSeamarks = L.tileLayer('https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png', {
    attribution: 'Map data: &copy; <a href="http://www.openseamap.org">OpenSeaMap</a> contributors'
}).addTo(map);

// Define base layers

let openSeaMap = L.tileLayer('https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png', {
    attribution: 'Map data: &copy; <a href="http://www.openseamap.org">OpenSeaMap</a> contributors'
});

let cartoLight = L.tileLayer(`https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png?key=${CARTO_API_KEY}`, {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
    maxZoom: 18,
    className: 'map-tiles'
});

let cartoDark = L.tileLayer(`https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png?key=${CARTO_API_KEY}`, {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
    maxZoom: 18,
    className: 'map-tiles'
});

let cartoVoyager = L.tileLayer(`https://{s}.basemaps.cartocdn.com/rastertiles/voyager_labels_under/{z}/{x}/{y}{r}.png?key=${CARTO_API_KEY}`, {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
    maxZoom: 18,
    className: 'map-tiles'
});

// Define weather overlay layers using OWM tile URLs directly
let cloud = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/CL/{z}/{x}/{y}?appid=${OWM_API_KEY}`);
let wind = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/WND/{z}/{x}/{y}?use_norm=false&opacity=0.9&arrow_step=16&appid=${OWM_API_KEY}`);
let pressure = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/APM/{z}/{x}/{y}?appid=${OWM_API_KEY}`);
let temperature = L.tileLayer(`https://{s}.sat.owm.io/vane/2.0/weather/TA2/{z}/{x}/{y}?appid=${OWM_API_KEY}`);
let rainfall = L.tileLayer(`https://tile.openweathermap.org/map/precipitation_new/{z}/{x}/{y}?appid=${OWM_API_KEY}`);
let snow = L.tileLayer(`https://tile.openweathermap.org/map/snow/{z}/{x}/{y}.png?appid=${OWM_API_KEY}`, {
    opacity: 0.5,
});

// Get current date and time in UTC for radar
let now = new Date();
now.setHours(now.getHours() - 1);
now.setMinutes(0);
now.setSeconds(0);
now.setMilliseconds(0);
let today = now.toISOString().slice(0, 16);

let precipitationRadar = L.tileLayer(`https://{s}.sat.owm.io/maps/2.0/radar/{z}/{x}/{y}?appid=${OWM_API_KEY}&day=${today}`);

// Weather layers are NOT added to map by default
let weather = L.layerGroup();
weather.addLayer(cloud);
weather.addLayer(wind);
weather.addLayer(pressure);
weather.addLayer(temperature);
weather.addLayer(precipitationRadar);
weather.addLayer(rainfall);
weather.addLayer(snow);


// Fetch helper with error handling
const fetchData = (pathToFile) => {
    return fetch(pathToFile)
        .then(response => {
            if (!response.ok) throw new Error(`HTTP ${response.status} loading ${pathToFile}`);
            return response.json();
        });
};

// GeoJSON Layers - 12nm and baseline
let twelveNm = L.geoJSON(null, {
    onEachFeature: function (feature, layer) {
        layer.bindPopup(`<b>12nm EEZ</b><br>Territory: ${feature.properties.TERRITORY1} (${feature.properties.ISO_TER1})<br>Sovereign: ${feature.properties.SOVEREIGN1} (${feature.properties.ISO_SOV1})<br>Area: ${feature.properties.AREA_KM2} km<sup>2</sup>`);
    },
    style: {
        opacity: 0,
        fillOpacity: 0,
    }
});

function load12nm() {
    spinner.spin(spinElement);
    (async () => {
        try {
            const data = await fetchData('./static/data/eez_12nm_v3.geojson');
            spinner.stop();
            let geoJsonLayer = L.geoJson.vt(data, {
                maxZoom: 18,
                tolerance: 5,
                debug: 0,
                style: (feature) => {
                    return {
                        color: '#0074D9',
                        weight: 2,
                        opacity: 1,
                        fillOpacity: 0.5,
                        fillColor: '#0074D9',
                    };
                },
            });
            geoJsonLayer.addTo(twelveNm);
            twelveNm.addData(data);
        } catch (e) {
            spinner.stop();
            console.warn('Failed to load 12nm EEZ data:', e.message);
            notie.alert({ type: 'warning', text: '12nm EEZ data is not available', time: 3 });
        }
    })();
}

// ECA SOx Areas
let ecaSOx = L.geoJSON(null, {
    onEachFeature: function (feature, layer) {
        layer.bindPopup(`<b>SOx ECA</b><br>Area: ${feature.properties.area}<br>Regulation: ${feature.properties.regulation}`);
    },
    style: {
        opacity: 0,
        fillOpacity: 0,
    }
});

function loadEcaSOx() {
    spinner.spin(spinElement);
    (async () => {
        try {
            const data = await fetchData('./static/data/eca_reg14_sox_pm.geojson');
            spinner.stop();
            let geoJsonLayer = L.geoJson.vt(data, {
                maxZoom: 18,
                tolerance: 5,
                debug: 0,
                style: (feature) => {
                    return {
                        color: '#ffdb00',
                        weight: 2,
                        opacity: 1,
                        fillOpacity: 0.5,
                        fillColor: '#f8d802',
                    };
                }
            });
            geoJsonLayer.addTo(ecaSOx);
            ecaSOx.addData(data);
        } catch (e) {
            spinner.stop();
            console.warn('Failed to load ECA SOx data:', e.message);
            notie.alert({ type: 'warning', text: 'ECA SOx data is not available', time: 3 });
        }
    })();
}

// ECA NOx Areas
let ecaNOx = L.geoJSON(null, {
    onEachFeature: function (feature, layer) {
        layer.bindPopup(`<b>NOx ECA</b><br>Area: ${feature.properties.area}`);
    },
    style: {
        opacity: 0,
        fillOpacity: 0,
    }
});

function loadEcaNOx() {
    spinner.spin(spinElement);
    (async () => {
        try {
            const data = await fetchData('./static/data/eca_reg13_nox.geojson');
            spinner.stop();
            let geoJsonLayer = L.geoJson.vt(data, {
                maxZoom: 18,
                tolerance: 5,
                debug: 0,
                style: (feature) => {
                    return {
                        color: '#FF4136',
                        weight: 2,
                        opacity: 1,
                        fillOpacity: 0.5,
                        fillColor: '#FF4136',
                    };
                }
            });
            geoJsonLayer.addTo(ecaNOx);
            ecaNOx.addData(data);
        } catch (e) {
            spinner.stop();
            console.warn('Failed to load ECA NOx data:', e.message);
            notie.alert({ type: 'warning', text: 'ECA NOx data is not available', time: 3 });
        }
    })();
}

// EEZ Baseline
let eezArea = L.featureGroup();

function loadEezArea() {
    spinner.spin(spinElement);
    (async () => {
        try {
            const data = await fetchData('./static/data/eez_v11.geojson');
            spinner.stop();
            let geoJsonLayer = L.geoJson.vt(data, {
                maxZoom: 18,
                tolerance: 5,
                debug: 0,
                style: (feature) => {
                    return {
                        color: '#2ECC40',
                        weight: 2,
                        opacity: 1,
                        fillOpacity: 0.5,
                        fillColor: '#2ECC40',
                    };
                }
            });
            geoJsonLayer.addTo(eezArea);
        } catch (e) {
            spinner.stop();
            console.warn('Failed to load EEZ data:', e.message);
            notie.alert({ type: 'warning', text: 'EEZ data is not available', time: 3 });
        }
    })();
}

// Clustered markers - Piracy Risk Areas
let alertIcon = L.icon({
    iconUrl: './static/icons/alert.svg',
    iconSize: [15, 15],
    iconAnchor: [10, 10],
    popupAnchor: [0, -15]
});

let piracyMarkerGroup = L.markerClusterGroup();

// Note: piracy layer is NOT added to map initially (lazy load via overlay toggle)
let piracy = L.geoJSON(null, {
    pointToLayer: function(feature, latlng) {
        return L.marker(latlng, {icon: alertIcon});
    },
    onEachFeature: function(feature, layer) {
        let popupContent = '<ul>';
        for (let prop in feature.properties) {
            popupContent += '<li><b>' + prop + ':</b> ' + feature.properties[prop] + '</li>';
        }
        popupContent += '</ul>';
        layer.bindPopup(popupContent, {maxHeight: 200});
        piracyMarkerGroup.addLayer(layer);
    }
});

// Piracy data loaded lazily (not at startup)
let piracyDataLoaded = false;
function loadPiracyData() {
    if (piracyDataLoaded) return;
    (async () => {
        try {
            const response = await fetch('./static/data/ASAM_events.geojson');
            if (!response.ok) { console.warn('Piracy data not available'); return; }
            const data = await response.json();
            piracy.addData(data);
            piracyDataLoaded = true;
        } catch(e) {
            console.warn('Failed to load piracy data:', e);
        }
    })();
}

// Custom icon for ports
let portIcon = L.icon({
    iconUrl: './static/icons/location-pin.svg',
    iconSize: [20, 20],
    iconAnchor: [10, 10],
    popupAnchor: [0, -15]
});

let portMarkerGroup = L.markerClusterGroup();

// Note: worldSeaPort layer is NOT added to map initially (lazy load)
let worldSeaPort = L.geoJSON(null, {
    pointToLayer: function(feature, latlng) {
        return L.marker(latlng, {icon: portIcon});
    },
    onEachFeature: function(feature, layer) {
        let popupContent = '<ul>';
        for (let prop in feature.properties) {
            popupContent += '<li><b>' + prop + ':</b> ' + feature.properties[prop] + '</li>';
        }
        popupContent += '</ul>';
        layer.bindPopup(popupContent, {maxHeight: 200});
        portMarkerGroup.addLayer(layer);
    }
});

// Port data loaded lazily
let portDataLoaded = false;
function loadPortData() {
    if (portDataLoaded) return;
    (async () => {
        try {
            const response = await fetch('./static/data/WPI.geojson');
            if (!response.ok) { console.warn('Port data not available'); return; }
            const data = await response.json();
            worldSeaPort.addData(data);
            portDataLoaded = true;
        } catch(e) {
            console.warn('Failed to load port data:', e);
        }
    })();
}


// Add layer control
let baseMaps = {
    "OpenStreetMap": osm,
    "OpenSeaMap": openSeaMap,
};
if (CARTO_API_KEY) {
    baseMaps["Carto Light"] = cartoLight;
    baseMaps["Carto Dark"] = cartoDark;
    baseMaps["Carto Voyager"] = cartoVoyager;
}
// Carto Voyager is the default base map; without a key its tiles are watermarked
(CARTO_API_KEY ? cartoVoyager : osm).addTo(map);
openSeaMapSeamarks.bringToFront();

let weatherLayers = {
    "Cloud": cloud,
    "Wind": wind,
    "Pressure": pressure,
    "Temperature": temperature,
    "Radar": precipitationRadar,
    "Precipitation": rainfall,
    "Snow": snow,
};

let marineLayers = {
    "Baseline & 12nm": twelveNm,
    "ECA SOx": ecaSOx,
    "ECA NOx": ecaNOx,
    "Piracy Incidents": piracyMarkerGroup,
    "World Seaports": portMarkerGroup,
    "EEZ Area": eezArea,
};

// Add a scale to the map
L.control.scale({
    imperial: true,
}).addTo(map);

// Add layer controls to the map
L.control.layers(baseMaps, weatherLayers).addTo(map);
L.control.layers(null, marineLayers, {
    collapsed: true,
}).addTo(map);

// Load data lazily when overlays are toggled on
map.on('overlayadd', function(e) {
    if (e.name === 'Baseline & 12nm') {
        load12nm();
    }
    if (e.name === 'ECA SOx') {
        loadEcaSOx();
    }
    if (e.name === 'ECA NOx') {
        loadEcaNOx();
    }
    if (e.name === 'EEZ Area') {
        loadEezArea();
    }
    if (e.name === 'Piracy Incidents') {
        loadPiracyData();
    }
    if (e.name === 'World Seaports') {
        loadPortData();
    }
});

// Show a popup when the user clicks on the map
map.on('click', function(event) {
    let latlng = event.latlng.wrap();
    let lat = latlng.lat;
    let lng = latlng.lng;

    let message = CleaCoordinates(lat, lng);

    notie.alert({
        type: 'info',
        text: message,
        time: 5
    });
});

// Define a variable to store the previously added waypoints
let previousWaypoints = null;
let fileName = null;
let currentGeoJsonData = null;

// Get the form and add an event listener
const form = document.querySelector("form");

form.addEventListener('submit', function(event) {
    event.preventDefault();

    if (!this.checkValidity()) {
        return;
    }

    // Get the values of the input fields
    const fromPortElement = document.getElementById('from-input');
    const toPortElement = document.getElementById('to-input');
    const fromPort = fromPortElement.value;
    const toPort = toPortElement.value;

    // Check that the input fields are not empty
    if (fromPort.trim() === '' || toPort.trim() === '') {
        notie.alert({
            type: 'error',
            text: 'Please enter "From" and "To" ports.',
            time: 3
        });
        return;
    }

    const formData = {
        fromPort: fromPort,
        toPort: toPort
    };

    // Send a request to the Go server with the form data
    fetch("/waypoints", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify(formData)
    })
    .then((response) => {
        if (!response.ok) {
            return response.json().then(err => {
                throw new Error(err.error || 'Request failed');
            });
        }
        return response.json();
    })
    .then((data) => {
        // Remove the previous waypoints from the map, if any
        if (previousWaypoints) {
            previousWaypoints.forEach(function(waypoint) {
                map.removeLayer(waypoint);
            });
        }

        // Remove previous route layer
        map.eachLayer(function(layer) {
            if (layer.options && layer.options.id === 'routeLayer') {
                map.removeLayer(layer);
            }
        });

        // Add the feature collection as a layer to the map
        var routeLayer = L.geoJSON(data, {
            id: 'routeLayer',
            style: function(feature) {
                return {
                    color: 'red'
                };
            },
            onEachFeature: function(feature, layer) {
                var properties = feature.properties;
                let routeName = properties.route_name;
                let distance = properties.total_dist;
                let distanceRounded = distance.toFixed(2);
                let distanceInNauticalMiles = (distance * 0.539957).toFixed(2);
                layer.bindPopup(routeName +
                    '<br>Total Distance: ' + distanceRounded + ' km / ' + distanceInNauticalMiles + ' nm'
                );
            }
        }).addTo(map);

        let waypoints = data.features[0].geometry.coordinates;
        let waypointIcon = L.icon({
            iconUrl: './static/icons/waypoint.png',
            iconSize: [8, 8]
        });

        let newWaypoints = [];
        waypoints.forEach(function(waypoint) {
            let marker = L.marker([waypoint[1], waypoint[0]], {
                icon: waypointIcon
            }).addTo(map);

            let coords = CleaCoordinates(waypoint[1], waypoint[0]);
            marker.bindPopup(coords);
            newWaypoints.push(marker);
        });

        // Store the new waypoints
        previousWaypoints = newWaypoints;
        currentGeoJsonData = data;
        fileName = `${fromPort}-${toPort}.geojson`;

        // Fit the map to the layer bounds
        map.fitBounds(routeLayer.getBounds());

        // Enable the download and clear buttons
        document.getElementById('download-button').removeAttribute("disabled");
        document.getElementById('clear-button').removeAttribute("disabled");
    })
    .catch((error) => {
        notie.alert({
            type: 'error',
            text: error.message || 'Failed to calculate route',
            time: 5
        });
    });
});

// Input validation
const fromPortElement = document.getElementById('from-input');
const toPortElement = document.getElementById('to-input');

fromPortElement.addEventListener('input', validateInput);
toPortElement.addEventListener('input', validateInput);

function validateInput() {
    const fromPort = fromPortElement.value;
    const toPort = toPortElement.value;

    if (fromPort.trim() === '' || toPort.trim() === '') {
        if (fromPort === '') {
            fromPortElement.setCustomValidity('Please enter a valid "From" port');
            fromPortElement.classList.add('is-invalid');
        } else {
            fromPortElement.setCustomValidity('');
            fromPortElement.classList.remove('is-invalid');
        }
        if (toPort === '') {
            toPortElement.setCustomValidity('Please enter a valid "To" port');
            toPortElement.classList.add('is-invalid');
        } else {
            toPortElement.setCustomValidity('');
            toPortElement.classList.remove('is-invalid');
        }
    } else {
        fromPortElement.setCustomValidity('');
        fromPortElement.classList.remove('is-invalid');
        toPortElement.setCustomValidity('');
        toPortElement.classList.remove('is-invalid');
    }
}


// Download button
const downloadButton = document.getElementById('download-button');
downloadButton.addEventListener('click', function() {
    const dataStr = 'data:text/json;charset=utf-8,' + encodeURIComponent(JSON.stringify(currentGeoJsonData));
    const downloadAnchorNode = document.createElement('a');
    downloadAnchorNode.setAttribute('href', dataStr);
    downloadAnchorNode.setAttribute('download', fileName);
    document.body.appendChild(downloadAnchorNode);
    downloadAnchorNode.click();
    downloadAnchorNode.remove();
});


// Clear button
const clearButton = document.getElementById('clear-button');

clearButton.addEventListener('click', function() {
    // Clear the input fields
    document.getElementById('from-input').value = '';
    document.getElementById('to-input').value = '';

    // Remove the previous waypoints from the map
    if (previousWaypoints) {
        previousWaypoints.forEach(function(waypoint) {
            map.removeLayer(waypoint);
        });
        previousWaypoints = null;
    }

    // Remove the route layer from the map
    map.eachLayer(function(layer) {
        if (layer.options && layer.options.id === 'routeLayer') {
            map.removeLayer(layer);
        }
    });

    // Reset the map to the initial view
    map.setView([0, 0], 2);

    // Disable buttons
    document.getElementById('download-button').setAttribute('disabled', true);
    clearButton.setAttribute('disabled', true);
});

// Disable the "Clear" button initially
clearButton.setAttribute('disabled', true);


// Add autocomplete functionality to the input fields
function setupAutocomplete(input, autocompleteItems) {
    input.addEventListener('input', function () {
        let value = this.value;

        if (!value) {
            autocompleteItems.innerHTML = '';
            return;
        }

        // Fetch data from the backend
        fetch('/ports?search=' + encodeURIComponent(value))
            .then(response => response.json())
            .then(data => {
                autocompleteItems.innerHTML = '';
                if (data && data.length > 0) {
                    data.forEach(function (item) {
                        const div = document.createElement('div');
                        div.textContent = `${item.port} (${item.country})`;
                        div.addEventListener('click', function () {
                            input.value = item.port;
                            autocompleteItems.innerHTML = '';
                        });
                        autocompleteItems.appendChild(div);
                    });
                }
            })
            .catch(err => console.warn('Autocomplete error:', err));
    });
}

const fromInput = document.getElementById('from-input');
const autocompleteItems = document.querySelector('.from-autocomplete-items');
setupAutocomplete(fromInput, autocompleteItems);

const toInput = document.getElementById('to-input');
const toAutocompleteItems = document.querySelector('.to-autocomplete-items');
setupAutocomplete(toInput, toAutocompleteItems);

// Close the autocomplete lists when the user clicks outside
document.addEventListener('click', function(e) {
    if (!e.target.matches('.autocomplete-items div')) {
        autocompleteItems.innerHTML = '';
        toAutocompleteItems.innerHTML = '';
    }
});
