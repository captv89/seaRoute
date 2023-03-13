// Create a map and add it to the page
const map = L.map("map").setView([0, 0], 2);
L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
}).addTo(map);

map.on('click', function(event) {
    var latlng = event.latlng;
    var message = `Coordinates: [${latlng.lat}, ${latlng.lng}]`;
    alert(message);
});

// Validation to impose limits on latitude and longitude
function validateLatitude(input) {
    var value = input.value;
    if (value < -90 || value > 90) {
        input.setCustomValidity("Latitude must be between -90 and 90");
    } else {
        input.setCustomValidity("");
    }
}

function validateLongitude(input) {
    var value = input.value;
    if (value < -180 || value > 180) {
        input.setCustomValidity("Longitude must be between -180 and 180");
    } else {
        input.setCustomValidity("");
    }
}

// Define a variable to store the previously added waypoints
var previousWaypoints = null;

// Get the form and add an event listener
const form = document.querySelector("form");

form.addEventListener('submit', function(event) {
    if (!this.checkValidity()) {
        event.preventDefault();
    } else {
            event.preventDefault();

            // Get the values of the form fields
            const originLatInput = document.getElementById("origin-lat");
            const originLngInput = document.getElementById("origin-lng");
            const destLatInput = document.getElementById("dest-lat");
            const destLngInput = document.getElementById("dest-lng");

            const formData = {};
            formData.originLatitude = originLatInput.value;
            formData.originLongitude = originLngInput.value;
            formData.destinationLatitude = destLatInput.value;
            formData.destinationLongitude = destLngInput.value;

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
                            layer.bindPopup('This Route!' +
                                '<br>Total Distance: ' + properties.total_dist + ' km'
                            );
                        }
                    }).addTo(map);

                    var waypoints = data.features[0].geometry.coordinates;
                    console.log(waypoints);
                    var waypointIcon = L.icon({
                        iconUrl: './static/icons/waypoint.png',
                        iconSize: [8, 8]
                    });

                    var newWaypoints = [];
                    waypoints.forEach(function(waypoint) {
                        var marker = L.marker([waypoint[1], waypoint[0]], {
                            icon: waypointIcon
                        }).addTo(map);
                        marker.bindPopup(`Coordinates: [${waypoint[1]}, ${waypoint[0]}]`);
                        newWaypoints.push(marker);
                    });

                    // Store the new waypoints in the previousWaypoints variable
                    previousWaypoints = newWaypoints;

                    // Fit the map to the layer bounds
                    map.fitBounds(routeLayer.getBounds());

                });
        }
});

// Get the checkbox and add an event listener
function handleCoordCheckboxChange(checkbox) {
    var fromLatInput = document.getElementById('from-lat');
    var fromLngInput = document.getElementById('from-lng');
    var toLatInput = document.getElementById('to-lat');
    var toLngInput = document.getElementById('to-lng');

    if (checkbox.checked) {
        fromLatInput.disabled = false;
        fromLngInput.disabled = false;
        toLatInput.disabled = false;
        toLngInput.disabled = false;
    } else {
        fromLatInput.disabled = true;
        fromLngInput.disabled = true;
        toLatInput.disabled = true;
        toLngInput.disabled = true;
    }
}

function showCoordinates() {
    var checkbox = document.getElementById("use-coordinates");
    var coordinateFields = document.getElementById("coordinate-fields");
    if (checkbox.checked) {
        coordinateFields.style.display = "block";
    } else {
        coordinateFields.style.display = "none";
    }
}



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
                console.log(data);
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


