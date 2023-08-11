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
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	influxdb2write "github.com/influxdata/influxdb-client-go/v2/api/write"
)

const (
	reportingInterval   = 2 * time.Second
	influxTimeout       = 3 * time.Second
	influxAttempts      = 2
	influxMaxRetryDelay = 500 * time.Millisecond

	tagDeviceName = "device_name"

	statNoiseLevel          = "noise_level"
	statNoiseMonitorState   = "noise_monitor_state"
	statSonosPlayState      = "sonos_play_state"
	statSonosCallErrorCount = "sonos_call_error_count"
)

type MetricsReporter interface {
	Flush()
	Close()

	ReportNoiseLevelMeasurement(averageDb, medianDb float64)
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

func StartMetricsReporter(config MetricsConfig, escalator AsyncErrorEscalator, verbose bool) (MetricsReporter, error) {
	metrics := &metricsReporter{
		enabled:                    config.Enabled,
		bucket:                     config.InfluxBucket,
		deviceName:                 config.DeviceName,
		currentSonosPlayState:      false,
		currentNoiseMonitorState:   0,
		currentSonosCallErrorCount: 0,
	}

	if !config.Enabled {
		return metrics, nil
	}
	if config.InfluxUrl == "" || config.InfluxBucket == "" || config.DeviceName == "" {
		//goland:noinspection GoErrorStringFormat
		return nil, errors.New("Influx URL, bucket, and device name must be set")
	}
	authString := ""
	if config.InfluxUsername != "" || config.InfluxPassword != "" {
		authString = fmt.Sprintf("%s:%s", config.InfluxUsername, config.InfluxPassword)
	}
	metrics.influx = influxdb2.NewClient(config.InfluxUrl, authString)
	metrics.influxWriter = metrics.influx.WriteAPIBlocking("", metrics.bucket)

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

	metrics.influxFlushErrChan = escalator.RegisterPolicy(&ErrorCountThresholdPolicy{
		ErrorCount: 150,
		TimeWindow: 5 * time.Minute,
		Name:       ">= 50% Influx writes failed over 5m",
		Log:        true,
	})
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
				go metrics.Flush()
			}
		}
	}()

	return metrics, nil
}

type metricsReporter struct {
	enabled      bool
	influx       influxdb2.Client
	influxWriter influxdb2api.WriteAPIBlocking
	bucket       string
	deviceName   string

	flushTicker        *time.Ticker
	flushDoneChan      chan bool
	influxFlushErrChan chan error

	// buffers:
	bufferLock                 sync.Mutex
	currentSonosPlayState      bool
	currentNoiseMonitorState   MonitorState
	currentSonosCallErrorCount int
	currentNoiseLevelPoints    []*influxdb2write.Point
}

func (m *metricsReporter) Flush() {
	if !m.enabled {
		return
	}

	m.bufferLock.Lock()

	atTime := time.Now()
	points := m.currentNoiseLevelPoints
	points = append(points, influxdb2.NewPoint(
		statNoiseMonitorState,
		map[string]string{tagDeviceName: m.deviceName},
		map[string]interface{}{
			"state_NUM": int(m.currentNoiseMonitorState),
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
	points = append(points, influxdb2.NewPoint(
		statSonosCallErrorCount,
		map[string]string{tagDeviceName: m.deviceName},
		map[string]interface{}{
			"errors": m.currentSonosCallErrorCount,
		},
		atTime,
	))

	m.currentSonosCallErrorCount = 0
	m.currentNoiseLevelPoints = nil
	m.bufferLock.Unlock()

	go func() {
		if err := retry.Do(
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
				defer cancel()
				return m.influxWriter.WritePoint(ctx, points...)
			},
			retry.Attempts(influxAttempts),
			retry.MaxDelay(influxMaxRetryDelay),
		); err != nil {
			m.influxFlushErrChan <- fmt.Errorf("failed to write metrics to influx: %w", err)
		}
	}()
}

func (m *metricsReporter) Close() {
	if !m.enabled {
		return
	}
	m.Flush()
	m.flushTicker.Stop()
	m.flushDoneChan <- true
}

func (m *metricsReporter) ReportNoiseLevelMeasurement(average, median float64) {
	if !m.enabled {
		return
	}
	m.bufferLock.Lock()
	defer m.bufferLock.Unlock()

	if m.currentNoiseMonitorState == Starting {
		return
	}

	m.currentNoiseLevelPoints = append(m.currentNoiseLevelPoints, influxdb2.NewPoint(
		statNoiseLevel,
		map[string]string{tagDeviceName: m.deviceName},
		map[string]interface{}{
			"moving_avg_dB":    average,
			"moving_median_dB": median,
		},
		time.Now(),
	))
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
