// worker.js

self.onmessage = function(event) {
    fetch(event.data.url)
        .then(response => response.json())
        .then(data => {
            self.postMessage({ data: data});
        });
};
