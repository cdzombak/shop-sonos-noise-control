package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

type RunMonitorArgs struct {
	samplingInterval     time.Duration
	thresholdDb          float64
	thresholdSeconds     int
	iface                string
	targetSonosId        string
	dbFilterRange        string
	verbose              bool
	extraSeekBackSeconds int
}

func runMonitor(args RunMonitorArgs) error {
	if args.thresholdDb < 0 {
		return fmt.Errorf("given threshold dB %f is negative", args.thresholdDb)
	}
	if args.thresholdSeconds < 0 {
		return fmt.Errorf("given threshold seconds %d is negative", args.thresholdSeconds)
	}
	if args.samplingInterval < 0 {
		return fmt.Errorf("given sampling interval %s is negative", args.samplingInterval)
	}
	if args.iface == "" {
		return errors.New("given interface name is empty")
	}
	if args.targetSonosId == "" {
		return errors.New("given Sonos device ID is empty")
	}
	if args.extraSeekBackSeconds < 0 {
		return fmt.Errorf("given extra-seekback-sec %d is negative", args.extraSeekBackSeconds)
	}

	minDb := 0.0
	maxDb := 200.0
	if args.dbFilterRange != "" {
		dbFilterRangeParts := strings.Split(args.dbFilterRange, "-")
		if len(dbFilterRangeParts) != 2 {
			return fmt.Errorf("given dB filter range '%s' is invalid; must be two positive integers separated by a hyphen (-)", args.dbFilterRange)
		}
		minDbInt, err := strconv.ParseInt(dbFilterRangeParts[0], 10, 32)
		if err != nil || minDbInt < 0 {
			return fmt.Errorf("given dB filter range '%s' is invalid; must be two positive integers separated by a hyphen (-)", args.dbFilterRange)
		}
		maxDbInt, err := strconv.ParseInt(dbFilterRangeParts[1], 10, 32)
		if err != nil || maxDbInt < 0 {
			return fmt.Errorf("given dB filter range '%s' is invalid; must be two positive integers separated by a hyphen (-)", args.dbFilterRange)
		}
		if minDbInt > maxDbInt {
			minDb = float64(maxDbInt)
			maxDb = float64(minDbInt)
		} else {
			minDb = float64(minDbInt)
			maxDb = float64(maxDbInt)
		}
	}

	state := Starting

	samples := int(math.Round(float64((time.Duration(args.thresholdSeconds) * time.Second) / args.samplingInterval)))
	log.Printf("monitoring a window of %d seconds with sampling interval of %d ms\n", args.thresholdSeconds, args.samplingInterval.Milliseconds())
	log.Printf("using a moving average of %d samples\n", samples)

	monitor, err := StartNoiseLevelMonitor(samples, args.samplingInterval, minDb, maxDb)
	if err != nil {
		return err
	}

	const sonosPollInterval = 1 * time.Second
	sonos, err := StartSonosClient(args.iface, args.targetSonosId, sonosPollInterval, args.verbose)
	if err != nil {
		return err
	}

	log.Println("starting main control loop")
	ticker := time.NewTicker(args.samplingInterval)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				log.Println("exiting main control loop")
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
				if args.verbose {
					go log.Printf("[monitor] tick %s: noise level %.1f dB", t, noiseLevelDb)
				}

				switch state {
				case Starting:
					panic("this case should not be reachable")
				case Quiet:
					if noiseLevelDb >= args.thresholdDb {
						if sonos.IsPlaying() {
							go log.Printf("[monitor] noise level %.1f dB is above threshold; pausing Sonos", noiseLevelDb)
							state = LoudSonosWasPlaying
							go func() {
								if err := sonos.Pause(); err != nil {
									log.Printf("[monitor] tick %s: failed to pause Sonos: %s", t, err)
								}
							}()
						} else {
							go log.Printf("[monitor] noise level %.1f dB is above threshold; Sonos is not playing", noiseLevelDb)
							state = LoudSonosWasNotPlaying
						}
					}
				case LoudSonosWasPlaying:
					if noiseLevelDb < args.thresholdDb {
						go log.Printf("[monitor] noise level %.1f dB fell below threshold; resuming Sonos", noiseLevelDb)
						seekSeconds := -1 * (args.thresholdSeconds + args.extraSeekBackSeconds)
						go func() {
							if err := sonos.Seek(seekSeconds); err != nil {
								log.Printf("[monitor] tick %s: failed to seek Sonos %d seconds: %s", t, seekSeconds, err)
							}
							if err := sonos.Play(sonos.LastPlaybackSpeed()); err != nil {
								log.Printf("[monitor] tick %s: failed to resume Sonos: %s", t, err)
							}
						}()
						state = Quiet
					}
				case LoudSonosWasNotPlaying:
					if noiseLevelDb < args.thresholdDb {
						go log.Printf("[monitor] noise level %.1f dB fell below threshold; Sonos was not playing", noiseLevelDb)
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
