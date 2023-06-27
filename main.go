package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

const (
	samplingInterval = 250 * time.Millisecond
)

const (
	ExitSuccess = 0
	ExitError   = 1
)

func main() {
	runDiscovery := flag.Bool("discover", false, "search for Sonos devices")
	runTestSonos := flag.Bool("test-sonos", false, "run a test of controlling the selected Sonos device")
	verbose := flag.Bool("verbose", false, "increase log detail, including noise level every sampling interval")
	iface := flag.String("interface", "eth0", "network interface for Sonos UPnP discovery")
	sonosId := flag.String("sonos-uuid", "RINCON_347E5CF2B10401400", "UUID of Sonos device to control, eg. RINCON_347E5CF2B10401400")
	thresholdDb := flag.Float64("db", 75.0, "dB value considered loud")
	thresholdSeconds := flag.Int("sec", 3, "time window used for the moving noise level average")
	flag.Usage = func() {
		// TODO(cdzombak): add version & makefile
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "shop-noise-sonos-control version %s\n", "TKTKTK")
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "by Chris Dzombak <https://www.github.com/cdzombak>\n\n")
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "Usage:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *verbose {
		log.Println("verbose logging is enabled")
	}

	if *runDiscovery && *runTestSonos {
		log.Println("only one of -discover and -test-sonos may be passed")
		os.Exit(ExitError)
	}

	// TODO(cdzombak): actual Sonos integration

	if *runDiscovery {
		if err := runDeviceDiscovery(*iface, *verbose); err != nil {
			log.Println(err)
			os.Exit(ExitError)
		}
		os.Exit(ExitSuccess)
	}

	if *runTestSonos {
		if err := runSonosTest(*iface, *sonosId, *verbose); err != nil {
			log.Println(err)
			os.Exit(ExitError)
		}
		os.Exit(ExitSuccess)
	}

	if err := runMonitor(samplingInterval, *thresholdDb, *thresholdSeconds, *verbose); err != nil {
		log.Println(err)
		os.Exit(ExitError)
	}

	os.Exit(ExitSuccess)
}
