package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"github.com/avast/retry-go"
	"github.com/ianr0bkny/go-sonos"
	"github.com/ianr0bkny/go-sonos/ssdp"
	"github.com/ianr0bkny/go-sonos/upnp"
)

var ErrNotConnected = errors.New("Sonos device is not connected") //nolint:staticcheck

const (
	reconnectInterval            = 30 * time.Second
	consecutiveFailsToDisconnect = 5
)

func StartSonosClient(ifaceName, targetSonosId string, pollInterval time.Duration, metrics MetricsReporter, errChan chan error, verbose bool) SonosClient {
	client := &sonosClient{
		isPlaying:         false,
		lastPlaybackSpeed: "1",
		metrics:           metrics,
		connected:         false,
	}

	log.Println("starting Sonos client")

	if err := client.sonosConnect(ifaceName, targetSonosId, verbose); err != nil {
		log.Printf("Sonos device not found at startup: %s; will retry every %s", err, reconnectInterval)
	} else {
		log.Println("Sonos device connected")
	}

	client.reconnectDoneChan = make(chan bool)
	go client.reconnectLoop(ifaceName, targetSonosId, verbose)

	client.pollTicker = time.NewTicker(pollInterval)
	client.pollDoneChan = make(chan bool)
	go func() {
		consecutiveFails := 0
		for {
			select {
			case <-client.pollDoneChan:
				log.Println("exiting Sonos client loop")
				return
			case t := <-client.pollTicker.C:
				if !client.IsConnected() {
					continue
				}

				var info *upnp.TransportInfo
				if err := retry.Do(
					func() error {
						var err error
						info, err = getTransportInfoWithTimeout(1*time.Second, client.getSonos(), 0)
						if err != nil {
							metrics.ReportSonosCallError()
						}
						return err
					},
					retry.Attempts(2),
					retry.Delay(100*time.Millisecond),
					retry.MaxDelay(1*time.Second),
				); err != nil {
					consecutiveFails++
					if consecutiveFails >= consecutiveFailsToDisconnect {
						log.Printf("Sonos device lost after %d consecutive poll failures; will attempt reconnection", consecutiveFails)
						client.setDisconnected()
						consecutiveFails = 0
					} else {
						errChan <- fmt.Errorf("GetTransportInfo failed after retries: %w", err)
					}
					continue
				}

				consecutiveFails = 0
				isPlaying := info.CurrentTransportState == upnp.State_PLAYING
				client.playbackStateMutex.Lock()
				client.isPlaying = isPlaying
				if isPlaying {
					client.lastPlaybackSpeed = info.CurrentSpeed
				}
				client.playbackStateMutex.Unlock()
				if verbose {
					log.Printf("[sonos] tick %s: playback state %s; speed %s", t, info.CurrentTransportState, info.CurrentSpeed)
				}
				metrics.ReportSonosPlayState(isPlaying)
			}
		}
	}()

	return client
}

type SonosClient interface {
	IsPlaying() bool
	IsConnected() bool
	LastPlaybackSpeed() string

	Pause() error
	Play(speed string) error
	Seek(seconds int) error

	Close()
}

type sonosClient struct {
	isPlaying          bool
	lastPlaybackSpeed  string
	playbackStateMutex sync.Mutex

	connected bool
	connMutex sync.Mutex
	sonos     *sonos.Sonos

	pollTicker        *time.Ticker
	pollDoneChan      chan bool
	reconnectDoneChan chan bool

	metrics MetricsReporter
}

func (c *sonosClient) sonosConnect(ifaceName, targetSonosId string, verbose bool) error {
	mgr := ssdp.MakeManager()
	defer func(mgr ssdp.Manager) {
		if err := mgr.Close(); err != nil {
			log.Printf("failed to close SSDP manager: %s\n", err)
		}
	}(mgr)

	if !verbose {
		log.SetOutput(io.Discard)
		defer log.SetOutput(os.Stderr)
	}
	if err := mgr.Discover(ifaceName, "11209", false); err != nil {
		return fmt.Errorf("SSDP discovery on interface %s failed: %w", ifaceName, err)
	}

	log.SetOutput(os.Stderr)
	result := mgr.QueryServices(ssdp.ServiceQueryTerms{
		"schemas-upnp-org-MusicServices": -1,
	})
	var sonosDevice ssdp.Device
	if devices, ok := result["schemas-upnp-org-MusicServices"]; ok {
		for _, device := range devices {
			if verbose {
				log.Printf(
					"discovered: %s  %s  %s  %s  %s\n",
					device.Product(),
					device.ProductVersion(),
					device.Name(),
					device.Location(),
					device.UUID(),
				)
			}
			if string(device.UUID()) == targetSonosId {
				sonosDevice = device
				break
			}
		}
	}
	if sonosDevice == nil {
		return fmt.Errorf("failed to discover Sonos device with UUID '%s'", targetSonosId)
	}

	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	c.sonos = sonos.Connect(sonosDevice, nil, sonos.SVC_AV_TRANSPORT)
	c.connected = true
	c.metrics.ReportSonosConnected(true)
	return nil
}

func (c *sonosClient) reconnectLoop(ifaceName, targetSonosId string, verbose bool) {
	ticker := time.NewTicker(reconnectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.reconnectDoneChan:
			return
		case <-ticker.C:
			if c.IsConnected() {
				continue
			}
			if err := c.sonosConnect(ifaceName, targetSonosId, verbose); err != nil {
				log.Printf("Sonos reconnection attempt failed: %s", err)
			} else {
				log.Println("Sonos device reconnected")
			}
		}
	}
}

func (c *sonosClient) setDisconnected() {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	c.connected = false
	c.sonos = nil
	c.metrics.ReportSonosConnected(false)
}

func (c *sonosClient) IsConnected() bool {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	return c.connected
}

func (c *sonosClient) getSonos() *sonos.Sonos {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	return c.sonos
}

func (c *sonosClient) IsPlaying() bool {
	c.playbackStateMutex.Lock()
	defer c.playbackStateMutex.Unlock()
	if !c.IsConnected() {
		return false
	}
	return c.isPlaying
}

func (c *sonosClient) LastPlaybackSpeed() string {
	c.playbackStateMutex.Lock()
	defer c.playbackStateMutex.Unlock()
	return c.lastPlaybackSpeed
}

func (c *sonosClient) Pause() error {
	s := c.getSonos()
	if s == nil {
		return ErrNotConnected
	}
	if err := retry.Do(
		func() error {
			err := runWithTimeout("pause", 1*time.Second, func() error {
				return s.Pause(0)
			})
			if err != nil {
				c.metrics.ReportSonosCallError()
			}
			return err
		},
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(1*time.Second),
	); err != nil {
		return fmt.Errorf("pause call failed: %w", err)
	}
	return nil
}

func (c *sonosClient) Play(speed string) error {
	if speed == "" {
		speed = c.LastPlaybackSpeed()
	}
	if speed == "" {
		speed = "1"
	}

	s := c.getSonos()
	if s == nil {
		return ErrNotConnected
	}
	if err := retry.Do(
		func() error {
			err := runWithTimeout("play", 1*time.Second, func() error {
				return s.Play(0, speed)
			})
			if err != nil {
				c.metrics.ReportSonosCallError()
			}
			return err
		},
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(1*time.Second),
	); err != nil {
		return fmt.Errorf("play call failed: %w", err)
	}
	return nil
}

func (c *sonosClient) Seek(seconds int) error {
	s := c.getSonos()
	if s == nil {
		return ErrNotConnected
	}

	minusSign := ""
	if seconds < 0 {
		minusSign = "-"
	}

	absSeconds := int(math.Abs(float64(seconds)))
	if absSeconds >= 60 {
		log.Printf("seek requested for %d seconds; truncating to 59 seconds", seconds)
		absSeconds = 59
	}
	seekTimeStr := fmt.Sprintf("%s00:00:%02d", minusSign, absSeconds)

	if err := retry.Do(
		func() error {
			err := runWithTimeout("seek", 1*time.Second, func() error {
				return s.Seek(0, "TIME_DELTA", seekTimeStr)
			})
			if err != nil {
				c.metrics.ReportSonosCallError()
			}
			return err
		},
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(1*time.Second),
	); err != nil {
		return fmt.Errorf("seek call failed: %w", err)
	}
	return nil
}

func (c *sonosClient) Close() {
	c.pollTicker.Stop()
	c.pollDoneChan <- true
	c.reconnectDoneChan <- true
}

type upnpTransportInfoResult struct {
	retv *upnp.TransportInfo
	err  error
}

func getTransportInfoWithTimeout(timeout time.Duration, sonos *sonos.Sonos, instanceId uint32) (*upnp.TransportInfo, error) {
	if sonos == nil {
		return nil, ErrNotConnected
	}
	resultCh := make(chan upnpTransportInfoResult, 1)
	go func() {
		retv, err := sonos.GetTransportInfo(instanceId)
		resultCh <- upnpTransportInfoResult{retv, err}
	}()
	select {
	case <-time.After(timeout):
		return nil, fmt.Errorf("GetTransportInfo (instanceId %d) timed out after %s", instanceId, timeout)
	case result := <-resultCh:
		return result.retv, result.err
	}
}

func runWithTimeout(desc string, timeout time.Duration, f func() error) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- f()
	}()
	select {
	case <-time.After(timeout):
		return fmt.Errorf("function ('%s') timed out after %s", desc, timeout)
	case err := <-errCh:
		return err
	}
}
