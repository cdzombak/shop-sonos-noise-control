package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/avast/retry-go"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

const (
	reportingInterval = 2 * time.Second
	influxTimeout     = 2 * time.Second
	influxRetries     = 3

	tagDeviceName = "device_name"

	statAverageNoiseLevel   = "moving_average_noise_level"
	statSonosPlayState      = "sonos_play_state"
	statNoiseMonitorState   = "noise_monitor_state"
	statSonosCallErrorCount = "sonos_call_error_count"
)

type MetricsReporter interface {
	Flush()
	Close()

	ReportAverageNoiseLevel(db float64)
	ReportSonosPlayState(playing bool)
	ReportNoiseMonitorState(state MonitorState)
	ReportSonosCallError()
}

type MetricsConfig struct {
	Enabled bool

	DeviceName     string
	InfluxUrl      string
	InfluxBucket   string
	InfluxUsername string
	InfluxPassword string
}

func StartMetricsReporter(config MetricsConfig, verbose bool) (MetricsReporter, error) {
	metrics := &metricsReporter{
		enabled:                    config.Enabled,
		bucket:                     config.InfluxBucket,
		deviceName:                 config.DeviceName,
		currentAverageNoiseLevel:   0,
		currentSonosPlayState:      false,
		currentNoiseMonitorState:   0,
		currentSonosCallErrorCount: 0,
	}

	if !config.Enabled {
		return metrics, nil
	}
	if config.InfluxUrl == "" || config.InfluxBucket == "" || config.DeviceName == "" {
		//goland:noinspection GoErrorStringFormat
		return nil, errors.New("Influx URL, bucket and, device name must be set")
	}
	authString := ""
	if config.InfluxUsername != "" || config.InfluxPassword != "" {
		authString = fmt.Sprintf("%s:%s", config.InfluxUsername, config.InfluxPassword)
	}
	metrics.influx = influxdb2.NewClient(config.InfluxUrl, authString)

	ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
	defer cancel()
	health, err := metrics.influx.Health(ctx)
	if err != nil {
		return nil, fmt.Errorf("InfluxDB health check failed: %w", err)
	}
	if health.Status != "pass" {
		return nil, fmt.Errorf("InfluxDB did not pass health check: status %s; message '%s'", health.Status, *health.Message)
	}

	log.Println("starting metrics loop")

	metrics.flushTicker = time.NewTicker(reportingInterval)
	metrics.flushDoneChan = make(chan bool)
	go func() {
		for {
			select {
			case <-metrics.flushDoneChan:
				log.Println("exiting metrics loop")
				return
			case t := <-metrics.flushTicker.C:
				if verbose {
					log.Printf("[metrics] flushing metrics at tick %s", t)
				}
				metrics.Flush()
			}
		}
	}()

	return metrics, nil
}

type metricsReporter struct {
	enabled    bool
	influx     influxdb2.Client
	bucket     string
	deviceName string

	flushTicker   *time.Ticker
	flushDoneChan chan bool

	// buffers:
	bufferLock                 sync.Mutex
	currentAverageNoiseLevel   float64
	currentSonosPlayState      bool
	currentNoiseMonitorState   MonitorState
	currentSonosCallErrorCount int
}

func (m *metricsReporter) Flush() {
	if !m.enabled {
		return
	}

	influx := m.influx.WriteAPIBlocking("", m.bucket)
	m.bufferLock.Lock()
	atTime := time.Now()

	var points []*write.Point

	if m.currentNoiseMonitorState != Starting {
		points = append(points, influxdb2.NewPoint(
			statAverageNoiseLevel,
			map[string]string{tagDeviceName: m.deviceName},
			map[string]interface{}{
				"dB": m.currentAverageNoiseLevel,
			},
			atTime,
		))
		points = append(points, influxdb2.NewPoint(
			statNoiseMonitorState,
			map[string]string{tagDeviceName: m.deviceName},
			map[string]interface{}{
				"state_N": m.currentNoiseMonitorState,
			},
			atTime,
		))
		points = append(points, influxdb2.NewPoint(
			statSonosPlayState,
			map[string]string{tagDeviceName: m.deviceName},
			map[string]interface{}{
				"playing": m.currentSonosPlayState,
			},
			atTime,
		))
	}

	points = append(points, influxdb2.NewPoint(
		statSonosCallErrorCount,
		map[string]string{tagDeviceName: m.deviceName},
		map[string]interface{}{
			"errors": m.currentSonosCallErrorCount,
		},
		atTime,
	))

	m.currentSonosCallErrorCount = 0
	m.bufferLock.Unlock()

	if err := retry.Do(
		func() error {
			ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
			defer cancel()
			return influx.WritePoint(ctx, points...)
		},
		retry.Attempts(influxRetries),
	); err != nil {
		// TODO(cdzombak): apply "log and exit if 51% fail for 10m" error policy here
		log.Printf("failed to write metrics to influx: %s", err)
	}
}

func (m *metricsReporter) Close() {
	if !m.enabled {
		return
	}
	m.Flush()
	m.flushTicker.Stop()
	m.flushDoneChan <- true
}

func (m *metricsReporter) ReportAverageNoiseLevel(db float64) {
	if !m.enabled {
		return
	}
	m.bufferLock.Lock()
	defer m.bufferLock.Unlock()

	m.currentAverageNoiseLevel = db
}

func (m *metricsReporter) ReportSonosPlayState(playing bool) {
	if !m.enabled {
		return
	}
	m.bufferLock.Lock()
	defer m.bufferLock.Unlock()

	m.currentSonosPlayState = playing
}

func (m *metricsReporter) ReportNoiseMonitorState(state MonitorState) {
	if !m.enabled {
		return
	}
	m.bufferLock.Lock()
	defer m.bufferLock.Unlock()

	m.currentNoiseMonitorState = state
}

func (m *metricsReporter) ReportSonosCallError() {
	if !m.enabled {
		return
	}
	m.bufferLock.Lock()
	defer m.bufferLock.Unlock()

	m.currentSonosCallErrorCount++
}
