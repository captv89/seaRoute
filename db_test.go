package main

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// TestLoadPorts pins the port list read from dataset/port-list.sqlite, so a
// driver change cannot silently alter what the app serves.
func TestLoadPorts(t *testing.T) {
	ports, err := LoadPorts()
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 4301 {
		t.Fatalf("got %d ports, want 4301", len(ports))
	}
	want := Port{Country: "Netherlands (the)", Port: "Rotterdam", Latitude: 51.904383352111097, Longitude: 4.4424468168432654}
	found := false
	h := sha256.New()
	for _, p := range ports {
		if p == want {
			found = true
		}
		fmt.Fprintf(h, "%s|%s|%.17g|%.17g\n", p.Country, p.Port, p.Latitude, p.Longitude)
	}
	if !found {
		t.Errorf("Rotterdam row missing or changed")
	}
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != portsSHA256 {
		t.Errorf("port list fingerprint %s, want %s", got, portsSHA256)
	}
}

// portsSHA256 was taken with the mattn/go-sqlite3 (CGO) driver before the
// switch to modernc.org/sqlite.
const portsSHA256 = "0cae8879a7bd766a7055decee1a74702a83bb5efd3789db0f4faec35e3717231"
