// Command weather-fetch downloads the latest ECMWF forecast run into a
// directory once and prints the resulting store status as JSON.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/captv89/seaRoute/weather"
)

func main() {
	dir := flag.String("dir", "", "directory for run files (required)")
	base := flag.String("base", weather.DefaultBaseURL, "base URL of the ECMWF open data")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "weather-fetch: -dir is required")
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store, err := weather.NewStore(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "weather-fetch:", err)
		os.Exit(1)
	}
	err = store.Refresh(ctx, &weather.Fetcher{BaseURL: *base})
	out, _ := json.MarshalIndent(store.Status(), "", "  ")
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, "weather-fetch:", err)
		os.Exit(1)
	}
}
