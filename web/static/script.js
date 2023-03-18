// Create a map and add it to the page

// const southWest = L.latLng(-90, -180);
// const northEast = L.latLng(90, 180);
// const bounds = L.latLngBounds(southWest, northEast);

// const map = L.map('map', {
//     center: [0, 0],
//     zoom: 2,
//     maxBounds: bounds
// }).setView([0, 0], 2);

const map = L.map("map", {
    preferCanvas: true, // Improve performance on mobile devices and older browsers
}).setView([0, 0], 2);

// Add OpenStreetMap tile layer from CDN
// L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
//     attribution:
//         '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
// }).addTo(map);

// L.tileLayer('https://{s}.tile.openstreetmap.fr/hot/{z}/{x}/{y}.png', {
//     attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
//     className: 'map-tiles'
// }).addTo(map);
//
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

// L.tileLayer('http://{s}.tile.openweathermap.org/map/clouds/{z}/{x}/{y}.png?appid={apiKey}', {
//     maxZoom: 19,
//     attribution: 'Map data &copy; <a href="http://openweathermap.org">OpenWeatherMap</a>',
//     apiKey: secretKey,
//     opacity: 0.5
// }).addTo(map);
//
// L.tileLayer('http://{s}.tile.openweathermap.org/map/wind/{z}/{x}/{y}.png?appid={apiKey}', {
//     maxZoom: 19,
//     attribution: 'Map data &copy; <a href="http://openweathermap.org">OpenWeatherMap</a>',
//     apiKey: secretKey,
//     opacity: 0.5
// }).addTo(map);

// Show a popup when the user clicks on the map
map.on('click', function(event) {
    let latlng = event.latlng.wrap();
    let lat = latlng.lat;
    let lng = latlng.lng;

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
                        marker.bindPopup(`Coordinates: [${waypoint[1]}, ${waypoint[0]}]`);
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


