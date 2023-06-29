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

var version = "<dev>"

func main() {
	runDiscovery := flag.Bool("discover", false, "search for Sonos devices")
	runTestSonos := flag.Bool("test-sonos", false, "run a test of controlling the selected Sonos device")
	verbose := flag.Bool("verbose", false, "increase log detail, including noise level every sampling interval")
	ifaceName := flag.String("interface", "eth0", "network interface for Sonos UPnP discovery")
	targetSonosId := flag.String("sonos-uuid", "RINCON_347E5CF2B10401400", "UUID of Sonos device to control, eg. RINCON_347E5CF2B10401400")
	thresholdDb := flag.Float64("db", 75.0, "dB value considered loud")
	thresholdSeconds := flag.Int("sec", 2, "time window for the moving noise level average")
	extraSeekBackSeconds := flag.Int("extra-seekback-sec", 1, "after noise quiets, seek Sonos back by this amount _plus_ the moving average time window")
	dbFilterRange := flag.String("db-filter-range", "10-180", "dB values outside this range read from the ADC will be considered nonsense and discarded")
	metricsDeviceName := flag.String("metrics-device-name", "", "device name reported to InfluxDB")
	influxUrl := flag.String("metrics-influx-url", "http://192.168.1.63:8086", "InfluxDB instance for metrics reporting (leave blank for none)")
	influxBucket := flag.String("metrics-influx-bucket", "default", "InfluxDB bucket for metrics reporting")
	influxUsername := flag.String("metrics-influx-username", "", "InfluxDB username for metrics reporting")
	influxPassword := flag.String("metrics-influx-password", "", "InfluxDB password for metrics reporting")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "shop-noise-sonos-control version %s\n", version)
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

	if *runDiscovery {
		if err := runDeviceDiscovery(*ifaceName, *verbose); err != nil {
			log.Println(err)
			os.Exit(ExitError)
		}
	}

	if *runTestSonos {
		if err := runSonosTest(*ifaceName, *targetSonosId, *verbose); err != nil {
			log.Println(err)
			os.Exit(ExitError)
		}
	}

	if !*runDiscovery && !*runTestSonos {
		metricsConfig := MetricsConfig{
			Enabled:        *metricsDeviceName != "",
			DeviceName:     *metricsDeviceName,
			InfluxUrl:      *influxUrl,
			InfluxBucket:   *influxBucket,
			InfluxUsername: *influxUsername,
			InfluxPassword: *influxPassword,
		}

		if err := runMonitor(RunMonitorArgs{
			samplingInterval:     samplingInterval,
			thresholdDb:          *thresholdDb,
			thresholdSeconds:     *thresholdSeconds,
			iface:                *ifaceName,
			targetSonosId:        *targetSonosId,
			verbose:              *verbose,
			dbFilterRange:        *dbFilterRange,
			extraSeekBackSeconds: *extraSeekBackSeconds,
			metricsConfig:        metricsConfig,
		}); err != nil {
			log.Println(err)
			os.Exit(ExitError)
		}
	}

	os.Exit(ExitSuccess)
}
