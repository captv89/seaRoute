package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
)

type NavWarnings struct {
	BroadcastWarn []BroadcastWarn `json:"broadcast-warn"`
}
type BroadcastWarn struct {
	MsgYear         int         `json:"msgYear"`
	MsgNumber       int         `json:"msgNumber"`
	NavArea         string      `json:"navArea"`
	Subregion       string      `json:"subregion"`
	Text            string      `json:"text"`
	Status          string      `json:"status"`
	IssueDate       string      `json:"issueDate"`
	Authority       string      `json:"authority"`
	CancelDate      interface{} `json:"cancelDate"`
	CancelNavArea   interface{} `json:"cancelNavArea"`
	CancelMsgYear   interface{} `json:"cancelMsgYear"`
	CancelMsgNumber interface{} `json:"cancelMsgNumber"`
	Year            int         `json:"year"`
	Area            string      `json:"area"`
	Number          int         `json:"number"`
}

type FeatureCollection struct {
	Type     string     `json:"type"`
	Features []Features `json:"features"`
}
type Properties struct {
	Navarea   int    `json:"Navarea"`
	Number    int    `json:"Number"`
	Status    string `json:"Status"`
	Authority string `json:"Authority"`
	IssueDate string `json:"IssueDate"`
	MsgYear   int    `json:"MsgYear"`
	Message   string `json:"Message"`
}
type Geometry struct {
	Coordinates []float64 `json:"coordinates"`
	Type        string    `json:"type"`
}
type Features struct {
	Type       string     `json:"type"`
	Properties Properties `json:"properties"`
	Geometry   Geometry   `json:"geometry"`
	ID         int        `json:"id"`
}

func GetNavWarnings() NavWarnings {
	uri := "https://msi.om.east.paas.nga.mil/api/publications/broadcast-warn"

	//msgYear := "2023"

	// Set parameters
	values := url.Values{}
	//values.Set("msgYear", msgYear)
	values.Set("output", "json")

	// Load the server's certificate
	cert, err := os.ReadFile("./temp/pcf-east-router.om.east.paas.nga.mil.cer")
	if err != nil {
		log.Fatal(err)
	}

	// Create a certificate pool and add the server's certificate
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(cert); !ok {
		log.Fatal("failed to append certificate")
	}

	// Create transport with the certificate pool
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: pool,
		},
	}

	// Create http client with the transport
	client := &http.Client{
		Transport: transport,
	}

	// Request url with parameters
	uri = uri + "?" + values.Encode()

	// create a new HTTP request with the JSON parameters
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return NavWarnings{}
	}

	// set the request header to specify the JSON content type
	req.Header.Set("Content-Type", "application/json")

	// send the HTTP request
	//client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return NavWarnings{}
	}

	// print the response body
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println("Error closing response body:", err)
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		// Print the response status
		fmt.Println("Response status:", resp.Status)

		// Print the response headers
		fmt.Println("Response headers:", resp.Header)

		// Read the response body
		var buf bytes.Buffer
		_, err = io.Copy(&buf, resp.Body)
		if err != nil {
			fmt.Println(err)
			return NavWarnings{}
		}

		// Print the raw response data
		fmt.Println(buf.String())
		return NavWarnings{}
	}

	// variable to store the response body
	navWarnings := NavWarnings{}

	err = json.NewDecoder(resp.Body).Decode(&navWarnings)
	if err != nil {
		log.Panic("Error decoding response body:", err)
	}
	//fmt.Println(navWarnings)
	return navWarnings
}

// SaveNavWarnings saves the nav warnings to a json file
func SaveNavWarnings(navWarnings NavWarnings) {
	file, _ := json.MarshalIndent(navWarnings, "", " ")
	_ = os.WriteFile("dataset/navwarnings.json", file, 0644)
}
