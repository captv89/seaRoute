# Short Sea Route Finder

This web application allows users to find waypoints along a sea route between an origin and a destination. The waypoints and the route are displayed on a map together with the total distance.

## Screenshot
![](https://i.ibb.co/7K9jW90/Screenshot-2023-01-01-at-00-07-23.png)

## Prerequisites
- [Go](https://go.dev/)
- [Gin](https://gin-gonic.com/)
- [Leaflet](https://leafletjs.com/)
- [Materialize](https://materializecss.com/)

## Installation

Clone the repository:

```bash
git clone https://github.com/captv89/seaRoute.git
```

Navigate to the project directory:

```bash
cd seaRoute
```

Install the dependencies:

```go
go get github.com/gin-gonic/gin github.com/pitchinnate/golangGeojsonDijkstra github.com/kellydunn/golang-geo
```


Start the server:
```go
go run main.go
```

Open your browser and navigate to http://localhost:8080 to use the application.

## Usage

- Enter the latitude and longitude of the origin and destination in the form.
- Click the "Calculate" button to retrieve the waypoints and the route.
- The waypoints, route details and the total distance will be displayed in the map on clicking the waypoint and route respectively.

## References
Marnet Dataset from the [searoute](https://github.com/eurostat/searoute) project by eurostat.

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

## License
This project is licensed under the MIT License - see the LICENSE file for details.