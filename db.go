package main

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
)

type Port struct {
	Country   string  `json:"country"`
	Port      string  `json:"port"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// LoadPorts port data from the sqlite database
func LoadPorts() ([]Port, error) {
	var ports []Port
	// Open the database
	db, err := sql.Open("sqlite3", "./dataset/port-list.sqlite")
	if err != nil {
		return nil, err
	}
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Println(err)
		}
	}(db)

	// Query the database
	rows, err := db.Query("SELECT country, port, latitude, longitude FROM port_details")
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Println(err)
		}
	}(rows)

	// Iterate through the result set
	for rows.Next() {
		p := new(Port)
		err := rows.Scan(&p.Country, &p.Port, &p.Latitude, &p.Longitude)
		if err != nil {
			return nil, err
		}
		ports = append(ports, *p)
	}
	return ports, nil
}
