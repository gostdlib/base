package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Azure/retry/exponential"

	"github.com/tailscale/hujson"

	_ "embed"
)

var (
	attempts = flag.Int("attempts", -1, "Number of attempts to make, defaults to -1 which is until MaxInterval is reached")
	gostruct = flag.Bool("gostruct", false, "Print the Go struct for the time table instead of human readable")
)

//go:embed settings.hujson
var settings []byte

func main() {
	flag.Parse()

	fmt.Printf("Generating TimeTable for %d attempts and the following settings:\n%s\n\n", *attempts, string(settings))

	p := exponential.Policy{}

	// hujson is a superset of JSON allowing comments.
	buff, err := hujson.Standardize(settings)
	if err != nil {
		fmt.Println("Error standardizing settings with hujson:", err)
		os.Exit(1)
	}
	if err := json.Unmarshal(buff, &p); err != nil {
		fmt.Println("Error unmarshalling settings:", err)
		os.Exit(1)
	}

	_, err = exponential.New(exponential.WithPolicy(p))
	if err != nil {
		fmt.Println("Error creating new policy:", err)
		os.Exit(1)
	}
	if *gostruct {
		tt := p.TimeTable(*attempts)
		fmt.Println(tt.Litter())
		return
	}

	fmt.Println(p.TimeTable(*attempts))
}
