/**
 * Leaflet GeoJSON VT Plugin
 * Renders GeoJSON data using geojson-vt for high performance with large datasets.
 * Requires geojson-vt library to be loaded first.
 */
(function () {
    'use strict';

    L.GeoJSON.VT = L.GridLayer.extend({
        initialize: function (geojson, options) {
            L.setOptions(this, options);
            this.tileIndex = geojsonvt(geojson, Object.assign({
                maxZoom: this.options.maxZoom || 18,
                tolerance: this.options.tolerance || 3,
                extent: 4096,
                buffer: 64,
                debug: this.options.debug || 0,
            }, this.options));
        },

        createTile: function (coords) {
            var tile = L.DomUtil.create('canvas', 'leaflet-tile');
            var size = this.getTileSize();
            tile.width = size.x;
            tile.height = size.y;

            var ctx = tile.getContext('2d');
            var tileInfo = this.tileIndex.getTile(coords.z, coords.x, coords.y);

            if (!tileInfo || !tileInfo.features || tileInfo.features.length === 0) {
                return tile;
            }

            var features = tileInfo.features;
            for (var i = 0; i < features.length; i++) {
                this._drawFeature(ctx, features[i], size);
            }

            return tile;
        },

        _drawFeature: function (ctx, feature, size) {
            var style = this.options.style ? this.options.style(feature) : {};
            var type = feature.type;
            var geom = feature.geometry;
            var ratio = size.x / 4096;

            ctx.strokeStyle = style.color || '#000';
            ctx.lineWidth = style.weight || 1;
            ctx.globalAlpha = style.opacity !== undefined ? style.opacity : 1;

            ctx.beginPath();
            for (var j = 0; j < geom.length; j++) {
                var ring = geom[j];
                for (var k = 0; k < ring.length; k++) {
                    var x = ring[k][0] * ratio;
                    var y = ring[k][1] * ratio;
                    if (k === 0) ctx.moveTo(x, y);
                    else ctx.lineTo(x, y);
                }
                if (type === 3) ctx.closePath();
            }

            if (type === 2 || type === 3) ctx.stroke();

            if (type === 3) {
                ctx.fillStyle = style.fillColor || style.color || '#000';
                ctx.globalAlpha = style.fillOpacity !== undefined ? style.fillOpacity : 0.2;
                ctx.fill();
            }
        }
    });

    L.geoJson.vt = function (geojson, options) {
        return new L.GeoJSON.VT(geojson, options);
    };
})();
