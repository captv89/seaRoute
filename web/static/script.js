// Create a map and add it to the page

// const southWest = L.latLng(-90, -180);
// const northEast = L.latLng(90, 180);
// const bounds = L.latLngBounds(southWest, northEast);

// const map = L.map('map', {
//     center: [0, 0],
//     zoom: 2,
//     maxBounds: bounds
// }).setView([0, 0], 2);

const spinOps = {
    lines: 13, // The number of lines to draw
    length: 38, // The length of each line
    width: 17, // The line thickness
    radius: 45, // The radius of the inner circle
    scale: 1, // Scales overall size of the spinner
    corners: 1, // Corner roundness (0..1)
    speed: 1, // Rounds per second
    rotate: 0, // The rotation offset
    animation: 'spinner-line-fade-quick', // The CSS animation name for the lines
    direction: 1, // 1: clockwise, -1: counterclockwise
    color: '#ffffff', // CSS color or array of colors
    fadeColor: 'transparent', // CSS color or array of colors
    top: '50%', // Top position relative to parent
    left: '50%', // Left position relative to parent
    shadow: '0 0 1px transparent', // Box-shadow for the lines
    zIndex: 2000000000, // The z-index (defaults to 2e9)
    className: 'spinner', // The CSS class to assign to the spinner
    position: 'absolute', // Element positioning
}

const spinElement = document.getElementById('map');

// Create a new spinner instance
const spinner = new Spinner(spinOps).spin(spinElement);
spinner.stop();

// Clean Coordinates
function CleaCoordinates(lat,lng) {
    let absLat = Math.abs(lat);
    let absLng = Math.abs(lng);


    // Convert latitude to degrees, minutes, and direction (N/S)
    const latDeg = Math.floor(absLat);
    const latDegString = latDeg.toString().padStart(2, '0');
    const latMin = ((absLat - latDeg) * 60).toFixed(2);
    const latDir = lat >= 0 ? 'N' : 'S';

    // Convert longitude to degrees, minutes, and direction (E/W)
    const lngDeg = Math.floor(absLng);
    const lngDegString = lngDeg.toString().padStart(3, '0');
    const lngMin = ((absLng - lngDeg) * 60).toFixed(2);
    const lngDir = lng >= 0 ? 'E' : 'W';

    let message = `${latDegString}° ${latMin}' ${latDir}, ${lngDegString}° ${lngMin}' ${lngDir}`;
    return message;
}


const map = L.map("map", {
    preferCanvas: true, // Improve performance on mobile devices and older browsers
}).setView([0, 0], 2);

// L.tileLayer('https://stamen-tiles-{s}.a.ssl.fastly.net/watercolor/{z}/{x}/{y}.png', {
//     attribution: 'Map tiles by Stamen Design, under CC BY 3.0. Data by OpenStreetMap, under ODbL.',
//     maxZoom: 18,
// }).addTo(map);

// L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', {
//     attribution: 'Map tiles by Carto, under CC BY 3.0. Data by OpenStreetMap, under ODbL.',
//     maxZoom: 18,
//     className: 'map-tiles'
// }).addTo(map);
// console.log('secretKey: ' + secretKey);

L.tileLayer('https://{s}.basemaps.cartocdn.com/rastertiles/voyager_labels_under/{z}/{x}/{y}{r}.png', {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
    subdomains: 'abcd',
    maxZoom: 20
}).addTo(map);

L.tileLayer('https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png', {
    attribution: 'Map data: &copy; <a href="http://www.openseamap.org">OpenSeaMap</a> contributors'
}).addTo(map);

// Define base layers so we can reference them multiple times
let osm = L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    className: 'map-tiles'
});

let openSeaMap = L.tileLayer('https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png', {
    attribution: 'Map data: &copy; <a href="http://www.openseamap.org">OpenSeaMap</a> contributors'
});

let cartoLight = L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', {
    attribution: 'Map tiles by Carto, under CC BY 3.0. Data by OpenStreetMap, under ODbL.',
    maxZoom: 18,
    className: 'map-tiles'
});

let cartoDark = L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
    attribution: 'Map tiles by Carto, under CC BY 3.0. Data by OpenStreetMap, under ODbL.',
    maxZoom: 18,
    className: 'map-tiles'
});

let cartoVoyager = L.tileLayer('https://{s}.basemaps.cartocdn.com/rastertiles/voyager_labels_under/{z}/{x}/{y}{r}.png', {
    attribution: 'Map tiles by Carto, under CC BY 3.0. Data by OpenStreetMap, under ODbL.',
    maxZoom: 18,
    className: 'map-tiles'
});

// Define overlays

let snow = L.OWM.snow({
    appId: '9de243494c0b295cca9337e1e96b00e2',
    opacity: 0.5,
    showLegend: true,
});

// Cloud Layer
let cloud = L.tileLayer('https://{s}.sat.owm.io/vane/2.0/weather/CL/{z}/{x}/{y}?appid=9de243494c0b295cca9337e1e96b00e2').addTo(map);

// Wind Layer @ 10m
// L.tileLayer('https://{s}.sat.owm.io/vane/2.0/weather/WS10/{z}/{x}/{y}?appid=9de243494c0b295cca9337e1e96b00e2').addTo(map);

// Wind with Direction
let wind = L.tileLayer('https://{s}.sat.owm.io/vane/2.0/weather/WND/{z}/{x}/{y}?use_norm=false&opacity=0.9&arrow_step=16&appid=9de243494c0b295cca9337e1e96b00e2').addTo(map);

// Atmospheric Pressure
let pressure = L.tileLayer('https://{s}.sat.owm.io/vane/2.0/weather/APM/{z}/{x}/{y}?appid=9de243494c0b295cca9337e1e96b00e2').addTo(map);

// Temperature
let temperature = L.tileLayer('https://{s}.sat.owm.io/vane/2.0/weather/TA2/{z}/{x}/{y}?appid=9de243494c0b295cca9337e1e96b00e2').addTo(map);

// Rainfall
let rainfall = L.tileLayer('https://tile.openweathermap.org/map/precipitation_new/{z}/{x}/{y}?appid=9de243494c0b295cca9337e1e96b00e2').addTo(map);

// Get current date and time in UTC
let now = new Date();
// Reduce time by 3 hours and round off to nearest hour
now.setHours(now.getHours() - 1);
now.setMinutes(0);
now.setSeconds(0);
now.setMilliseconds(0);
// console.log('now: ' + now);
//  Convert today to format of 2023-03-20T20:20 in UTC
let today = now.toISOString().slice(0, 16);
// console.log('today: ' + today);

// Precipitation
let precipitationRadar = L.tileLayer(`https://{s}.sat.owm.io/maps/2.0/radar/{z}/{x}/{y}?appid=9de243494c0b295cca9337e1e96b00e2&day=${today}`).addTo(map);

// Add a layer group to the map
let weather = L.layerGroup().addTo(map);

// Add the weather layers to the layer group
weather.addLayer(cloud);
weather.addLayer(wind);
weather.addLayer(pressure);
weather.addLayer(temperature);
weather.addLayer(precipitationRadar);
weather.addLayer(rainfall);
weather.addLayer(snow);


// create a Promise that fetches the data
const fetchData = (pathToFile) => {
    return new Promise((resolve, reject) => {
        fetch(pathToFile)
            .then(response => response.json())
            .then(data => resolve(data))
            .catch(error => reject(error));
    });
};

// GeoJSON Layers
// 12nm and baseline
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
    // console.log('Loading load12nm');
    spinner.spin(spinElement);
// Async load GeoJSON data
    (async () => {
// const response = await fetch('./static/data/eez_12nm_v3.geojson');
//     const data = await response.json();
        const data = await fetchData('./static/data/eez_12nm_v3.geojson');

        // Check if data is loaded
        if (data != null) {
            // console.log('Data loaded');
            spinner.stop();
        }

        let geoJsonLayer = L.geoJson.vt(data, {
            maxZoom: 18,
            tolerance: 5,
            debug: 0,
            style: (feature) => {
                return {
                    color: '#0074D9', // blue
                    weight: 2,
                    opacity: 1,
                    fillOpacity: 0.5,
                    fillColor: '#0074D9', // blue
                };
            },
        });

        geoJsonLayer.addTo(twelveNm);

        // Add the layer data to ecaNOx to show on mouseover
        twelveNm.addData(data);
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
    // console.log('Loading loadEcaSOx');
    spinner.spin(spinElement);
// Async load GeoJSON data
    (async () => {
        // const response = await fetch('./static/data/eca_reg14_sox_pm.geojson');
        // const data = await response.json();
        const data = await fetchData('./static/data/eca_reg14_sox_pm.geojson');

        // Check if data is loaded
        if (data != null) {
            // console.log('Data loaded');
            spinner.stop();
        }

        let geoJsonLayer = L.geoJson.vt(data, {
            maxZoom: 18,
            tolerance: 5,
            debug: 0,
            style: (feature) => {
                return {
                    color: '#ffdb00', // red
                    weight: 2,
                    opacity: 1,
                    fillOpacity: 0.5,
                    fillColor: '#f8d802', // red
                };
            }
        });

        geoJsonLayer.addTo(ecaSOx);

        // Add the layer data to ecaNOx to show on mouseover
        ecaSOx.addData(data);

    })();

}

// // ECA NOx Areas
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
    // console.log('Loading loadEcaNOx');
    spinner.spin(spinElement);
// Async load GeoJSON data
    (async () => {
        // const response = await fetch('./static/data/eca_reg13_nox.geojson');
        // const data = await response.json();
        const data = await fetchData('./static/data/eca_reg13_nox.geojson');

        // Check if data is loaded
        if (data != null) {
            // console.log('Data loaded');
            spinner.stop();
        }

        let geoJsonLayer = L.geoJson.vt(data, {
            maxZoom: 18,
            tolerance: 5,
            debug: 0,
            style: (feature) => {
                return {
                    color: '#FF4136', // red
                    weight: 2,
                    opacity: 1,
                    fillOpacity: 0.5,
                    fillColor: '#FF4136', // red
                };
            }
        });
        geoJsonLayer.addTo(ecaNOx);

        // Add the layer data to ecaNOx to show on mouseover
        ecaNOx.addData(data);

    })();
}


// EEZ Baseline
let eezArea = L.featureGroup();

function loadEezArea() {
    // console.log('Loading loadEezArea');
    spinner.spin(spinElement);
// Async load GeoJSON data
    (async () => {
        // const response = await fetch('./static/data/eez_v11.geojson');
        // const data = await response.json();
        const data = await fetchData('./static/data/eez_v11.geojson');

        // Check if data is loaded
        if (data != null) {
            // console.log('Data loaded');
            spinner.stop();
        }

        let geoJsonLayer = L.geoJson.vt(data, {
            maxZoom: 18,
            tolerance: 5,
            debug: 0,
            style: (feature) => {
                return {
                    color: '#2ECC40', // green
                    weight: 2,
                    opacity: 1,
                    fillOpacity: 0.5,
                    fillColor: '#2ECC40', // green
                };
            }
        });
        geoJsonLayer.addTo(eezArea);
    })();
}
// Clustered markers
// Piracy Risk Areas
let alertIcon = L.icon({
    iconUrl: './static/icons/alert.svg',
    iconSize: [15, 15],
    iconAnchor: [10, 10],
    popupAnchor: [0, -15]
});

// Create a marker layer group
let piracyMarkerGroup = L.markerClusterGroup();

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
        let bounds = map.getBounds();
        if (!bounds.contains(layer.getLatLng())) {
            piracy.removeLayer(layer);
    }
}
}).addTo(map);

// Async load GeoJSON data
(async () => {
const response = await fetch('./static/data/ASAM_events.geojson');
    const data = await response.json();
    piracy.addData(data);
})();


// Custom icon options
let portIcon = L.icon({
    iconUrl: './static/icons/location-pin.svg',
    iconSize: [20, 20], // set the size of the icon
    iconAnchor: [10, 10], // set the anchor point of the icon
    popupAnchor: [0, -15] // set the anchor point of the popup
});

// Create a marker layer group
let portMarkerGroup = L.markerClusterGroup();

// Word seaport
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
        let bounds = map.getBounds();
        if (!bounds.contains(layer.getLatLng())) {
            worldSeaPort.removeLayer(layer);
        }
    }
}).addTo(map);

// Async load GeoJSON data
(async () => {
const response = await fetch('./static/data/WPI.geojson');
    const data = await response.json();
    worldSeaPort.addData(data);
})();



// Add layer control
let baseMaps = {
    "OpenStreetMap": osm,
    "OpenSeaMap": openSeaMap,
    "Carto Light": cartoLight,
    "Carto Dark": cartoDark,
    "Carto Voyager": cartoVoyager,
}

let weatherLayers = {
    "Cloud": cloud,
    "Wind": wind,
    "Pressure": pressure,
    "Temperature": temperature,
    "Radar": precipitationRadar,
    "Precipitation": rainfall,
    "Snow": snow,
}

let marineLayers = {
    "Baseline & 12nm": twelveNm,
    "ECA SOx": ecaSOx,
    "ECA NOx": ecaNOx,
    "Piracy Incidents": piracyMarkerGroup,
    "World Seaports": portMarkerGroup,
    "EEZ Area": eezArea,
}

// Disable all layers by default
wind.remove();
temperature.remove();
precipitationRadar.remove();
cloud.remove();
pressure.remove();
rainfall.remove();
snow.remove();

// twelveNm.remove();
// ecaSoX.remove();
// ecaNOx.remove();
// eezArea.remove();

piracyMarkerGroup.remove();
portMarkerGroup.remove();
piracy.remove();
worldSeaPort.remove();


// Add a scale to the map
L.control.scale({
    imperial: true,
}).addTo(map);

// Add a layer control to the map
L.control.layers(baseMaps, weatherLayers).addTo(map);

// Add a maritime layer control
L.control.layers(null, marineLayers, {
    collapsed: true,
}).addTo(map);

// Call load12nm function when the 12nm EEZ overlay is added
map.on('overlayadd', function(e) {
    // console.log(e);
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
});

// Show a popup when the user clicks on the map
map.on('click', function(event) {
    let latlng = event.latlng.wrap();
    let lat = latlng.lat;
    let lng = latlng.lng;

    let message = CleaCoordinates(lat,lng);

    console.log(message);
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
    if (!this.checkValidity()) {
        event.preventDefault();
    } else {
            event.preventDefault();

            // Get the values of the input fields
            const fromPortElement = document.getElementById('from-input');
            const toPortElement = document.getElementById('to-input');
            const fromPort = fromPortElement.value;
            const toPort = toPortElement.value;

            // // Do something with the values (e.g. pass them to your backend or API)
            // console.log('From: ' + fromPort);
            // console.log('To: ' + toPort);

            // Check that the input fields are not empty
            if (fromPort.trim() === '' || toPort.trim() === '') {
                notie.alert({
                    type: 'error',
                    text: 'Please enter "From" and "To" ports.',
                    time: 3
                });
                return;
            }

            // Get the values of the form fields
            // const originLatInput = document.getElementById("origin-lat");
            // const originLngInput = document.getElementById("origin-lng");
            // const destLatInput = document.getElementById("dest-lat");
            // const destLngInput = document.getElementById("dest-lng");

            const formData = {};
            // formData.originLatitude = originLatInput.value;
            // formData.originLongitude = originLngInput.value;
            // formData.destinationLatitude = destLatInput.value;
            // formData.destinationLongitude = destLngInput.value;
            formData.fromPort = fromPort;
            formData.toPort = toPort;

            // Send a request to the Go server with the form data
            fetch("/waypoints", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json"
                },
                body: JSON.stringify(formData)
            })
                .then((response) => response.json())
                .then((data) => {
                    // Do something with the response data
                    console.log(data);

                    // Remove the previous waypoints from the map, if any
                    if (previousWaypoints) {
                        previousWaypoints.forEach(function(waypoint) {
                            map.removeLayer(waypoint);
                        });
                    }

                    // To remove the layer, you can use the following code
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
                                '<br>Total Distance: ' + distanceRounded + ' km / '+ distanceInNauticalMiles + ' nm'
                            );
                        }
                    }).addTo(map);

                    let waypoints = data.features[0].geometry.coordinates;
                    // console.log(waypoints);
                    let waypointIcon = L.icon({
                        iconUrl: './static/icons/waypoint.png',
                        iconSize: [8, 8]
                    });

                    let newWaypoints = [];
                    waypoints.forEach(function(waypoint) {
                        let marker = L.marker([waypoint[1], waypoint[0]], {
                            icon: waypointIcon
                        }).addTo(map);

                        // Create a popup with the coordinates
                        let coords = CleaCoordinates(waypoint[1], waypoint[0]);
                        marker.bindPopup(coords);

                        newWaypoints.push(marker);
                    });

                    // Store the new waypoints in the previousWaypoints variable
                    previousWaypoints = newWaypoints;

                    currentGeoJsonData = data;
                    fileName = `${fromPort}-${toPort}.geojson`;

                    // Fit the map to the layer bounds
                    map.fitBounds(routeLayer.getBounds());

                    // Enable the download button
                    document.getElementById('download-button').removeAttribute("disabled");
                    // Enable the clear button
                    document.getElementById('clear-button').removeAttribute("disabled");
                });
        }
});

const fromPortElement = document.getElementById('from-input');
const toPortElement = document.getElementById('to-input');

// Add event listener to input fields
fromPortElement.addEventListener('input', validateInput);
toPortElement.addEventListener('input', validateInput);

function validateInput() {
    // Get the values of the input fields
    const fromPort = fromPortElement.value;
    const toPort = toPortElement.value;

    // Check that the input fields are not empty
    if (fromPort.trim() === '' || toPort.trim() === '') {
        // Add error styles to input fields
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
        // Remove error styles from input fields
        fromPortElement.setCustomValidity('');
        fromPortElement.classList.remove('is-invalid');
        toPortElement.setCustomValidity('');
        toPortElement.classList.remove('is-invalid');
    }
}



// Add a download button to allow users to download the GeoJSON data as a file
const downloadButton = document.getElementById('download-button');
downloadButton.addEventListener('click', function() {

    const dataStr = 'data:text/json;charset=utf-8,' + encodeURIComponent(JSON.stringify(currentGeoJsonData));
    const downloadAnchorNode = document.createElement('a');
    downloadAnchorNode.setAttribute('href', dataStr);
    downloadAnchorNode.setAttribute('download', fileName);
    document.body.appendChild(downloadAnchorNode); // required for firefox
    downloadAnchorNode.click();
    downloadAnchorNode.remove();


});


// Clear everything when the clear button is clicked
// Get the "Clear" button element
const clearButton = document.getElementById('clear-button');

// Add an event listener to the "Clear" button to clear the form and remove the previous waypoints and route layer
clearButton.addEventListener('click', function() {
    // Clear the input fields
    document.getElementById('from-input').value = '';
    document.getElementById('to-input').value = '';

    // Remove the previous waypoints from the map, if any
    if (previousWaypoints) {
        previousWaypoints.forEach(function(waypoint) {
            map.removeLayer(waypoint);
        });
        previousWaypoints = null;
    }

    // Remove the route layer from the map, if any
    map.eachLayer(function(layer) {
        if (layer.options && layer.options.id === 'routeLayer') {
            map.removeLayer(layer);
        }
    });

    // Clear the Active class in the label
    // Remove the active class from the label elements
    document.querySelectorAll('.active').forEach(function(label) {
        label.classList.remove('active');
    });

    // Reset the map to the initial view
    map.setView([0, 0], 2);

    // Disable the download button
    const downloadButton = document.getElementById('download-button');
    downloadButton.setAttribute('disabled', true);
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
        fetch('/ports?search=' + value)
            .then(response => response.json())
            .then(data => {
                autocompleteItems.innerHTML = '';
                // console.log(data);
                data.forEach(function (item) {
                    const div = document.createElement('div');
                    div.textContent = `${item.port}(${item.country})`;
                    div.addEventListener('click', function () {
                        input.value = item.port;
                        autocompleteItems.innerHTML = '';
                    });
                    autocompleteItems.appendChild(div);
                });
            });
    });
}


const fromInput = document.getElementById('from-input');
const autocompleteItems = document.querySelector('.from-autocomplete-items');
setupAutocomplete(fromInput, autocompleteItems);

const toInput = document.getElementById('to-input');
const toAutocompleteItems = document.querySelector('.to-autocomplete-items');
setupAutocomplete(toInput, toAutocompleteItems);


// Close the autocomplete list when the user clicks outside of it
document.addEventListener('click', function(e) {
    if (!e.target.matches('.autocomplete-items div')) {
        autocompleteItems.innerHTML = '';
    }
});
