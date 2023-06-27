package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type monitorState int

const (
	Starting monitorState = iota
	Quiet
	LoudSonosWasNotPlaying
	LoudSonosWasPlaying
)

func runMonitor(samplingInterval time.Duration, thresholdDb float64, thresholdSeconds int, verbose bool) error {
	if thresholdDb < 0 {
		return fmt.Errorf("given threshold dB %f is negative", thresholdDb)
	}
	if thresholdSeconds < 0 {
		return fmt.Errorf("given threshold seconds %d is negative", thresholdSeconds)
	}

	state := Starting

	samples := int(math.Round(float64((time.Duration(thresholdSeconds) * time.Second) / samplingInterval)))
	log.Printf("monitoring a window of %d seconds with sampling interval of %d ms\n", thresholdSeconds, samplingInterval.Milliseconds())
	log.Printf("using a moving average of %d samples\n", samples)

	monitor, err := StartNoiseLevelMonitor(samples, samplingInterval)
	if err != nil {
		return err
	}

	// TODO(cdzombak): make a real SonosConnection
	const sonosIsPlaying = true

	ticker := time.NewTicker(samplingInterval)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				log.Println("exiting monitor loop")
				return
			case t := <-ticker.C:
				if !monitor.Ready() {
					go log.Printf("[monitor] tick %s: monitor is not ready", t)
					continue
				}
				if state == Starting { // conditional immediately above continues loop if noise monitor is not ready
					state = Quiet
				}

				noiseLevelDb := monitor.ReadAverage()
				if verbose {
					go log.Printf("[monitor] tick %s: noise level %.1f dB", t, noiseLevelDb)
				}

				switch state {
				case Starting:
					panic("this case should not be reachable")
				case Quiet:
					if noiseLevelDb >= thresholdDb {
						if sonosIsPlaying {
							go log.Printf("[monitor] tick %s: noise level %.1f dB is above threshold; pausing Sonos", t, noiseLevelDb)
							state = LoudSonosWasPlaying
							// TODO(cdzombak): async pause Sonos
						} else {
							go log.Printf("[monitor] tick %s: noise level %.1f dB is above threshold; Sonos is not playing", t, noiseLevelDb)
							state = LoudSonosWasNotPlaying
						}
					}
				case LoudSonosWasPlaying:
					if noiseLevelDb < thresholdDb {
						go log.Printf("[monitor] tick %s: noise level %.1f dB fell below threshold; resuming Sonos", t, noiseLevelDb)
						// TODO(cdzombak): async resume Sonos
						state = Quiet
					}
				case LoudSonosWasNotPlaying:
					if noiseLevelDb < thresholdDb {
						go log.Printf("[monitor] tick %s: noise level %.1f dB fell below threshold; Sonos was not playing", t, noiseLevelDb)
						state = Quiet
					}
				}
			}
		}
	}()

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, os.Interrupt, syscall.SIGTERM)
	<-exitChan
	ticker.Stop()
	_ = monitor.Close()
	done <- true
	return nil
}
