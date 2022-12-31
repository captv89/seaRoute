// Create a map and add it to the page
const map = L.map("map").setView([0, 0], 2);
L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
}).addTo(map);


const form = document.querySelector("form");
form.addEventListener("submit", (event) => {
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

            // Add the feature collection as a layer to the map
            var routeLayer = L.geoJSON(data, {
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

            var waypointIcon = L.icon({
                iconUrl: './static/icons/waypoint.png',
                iconSize: [8, 8]
            });

            waypoints.forEach(function(waypoint) {
                var marker = L.marker([waypoint[1], waypoint[0]], {
                    icon: waypointIcon
                }).addTo(map);
                marker.bindPopup(`Coordinates: [${waypoint[1]}, ${waypoint[0]}]`);
            });

            // Fit the map to the layer bounds
            map.fitBounds(routeLayer.getBounds());

        });
});


