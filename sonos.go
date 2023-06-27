package main

import (
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
	"github.com/pkg/errors"
)

func StartSonosClient(iface, targetSonosId string, pollInterval time.Duration, verbose bool) (SonosClient, error) {
	client := &sonosClient{
		isPlaying:         false,
		lastPlaybackSpeed: "1",
	}

	if err := client.sonosConnect(iface, targetSonosId, verbose); err != nil {
		return nil, err
	}

	client.pollTicker = time.NewTicker(pollInterval)
	client.pollDoneChan = make(chan bool)
	go func() {
		for {
			select {
			case <-client.pollDoneChan:
				log.Println("exiting Sonos loop")
				return
			case t := <-client.pollTicker.C:
				var info *upnp.TransportInfo
				if err := retry.Do(
					func() error {
						var err error
						info, err = client.sonos.GetTransportInfo(0)
						return err
					},
					retry.Attempts(10),
					retry.Delay(500*time.Millisecond),
					retry.MaxDelay(5*time.Second),
				); err != nil {
					log.Fatalf("GetTransportInfo failed after 10 attempts: %s", err)
				}
				client.playbackStateMutex.Lock()
				client.isPlaying = info.CurrentTransportState == upnp.State_PLAYING
				client.lastPlaybackSpeed = info.CurrentSpeed
				client.playbackStateMutex.Unlock()
				if verbose {
					log.Printf("[sonos] tick %s: playback state %s; speed %s", t, info.CurrentTransportState, info.CurrentSpeed)
				}
			}
		}
	}()

	return client, nil
}

type SonosClient interface {
	IsPlaying() bool           // guaranteed to return fast; updated roughly every pollInterval
	LastPlaybackSpeed() string // // guaranteed to return fast; updated roughly every pollInterval

	Pause() error
	Play(speed string) error
	Seek(seconds int) error

	Close() error
}

type sonosClient struct {
	isPlaying          bool
	lastPlaybackSpeed  string
	playbackStateMutex sync.Mutex

	pollTicker   *time.Ticker
	pollDoneChan chan bool

	sonos *sonos.Sonos
}

func (c *sonosClient) sonosConnect(iface, targetSonosId string, verbose bool) error {
	if c.sonos != nil {
		panic("sonosConnect already called on this client")
	}

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
	if err := mgr.Discover(iface, "11209", false); err != nil {
		return errors.Wrapf(err, "SSDP discovery on interface %s failed", iface)
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

	c.sonos = sonos.Connect(sonosDevice, nil, sonos.SVC_AV_TRANSPORT)
	return nil
}

func (c *sonosClient) IsPlaying() bool {
	c.playbackStateMutex.Lock()
	defer c.playbackStateMutex.Unlock()
	return c.isPlaying
}

func (c *sonosClient) LastPlaybackSpeed() string {
	c.playbackStateMutex.Lock()
	defer c.playbackStateMutex.Unlock()
	return c.lastPlaybackSpeed
}

func (c *sonosClient) Pause() error {
	if err := retry.Do(
		func() error {
			return c.sonos.Pause(0)
		},
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(2*time.Second),
	); err != nil {
		return errors.Wrap(err, "pause call failed")
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

	if err := retry.Do(
		func() error {
			return c.sonos.Play(0, speed)
		},
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(2*time.Second),
	); err != nil {
		return errors.Wrap(err, "play call failed")
	}
	return nil
}

func (c *sonosClient) Seek(seconds int) error {
	absSeconds := int(math.Abs(float64(seconds)))
	if absSeconds >= 60 {
		// TODO(cdzombak): properly format seconds into HH:MM:SS, allowing arbitrary seek times
		return errors.New("only seek times less than 60s are supported")
	}
	minusSign := ""
	if seconds < 0 {
		minusSign = "-"
	}
	seekTimeStr := fmt.Sprintf("%s00:00:%02d", minusSign, absSeconds)

	if err := retry.Do(
		func() error {
			return c.sonos.Seek(0, "TIME_DELTA", seekTimeStr)
		},
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(2*time.Second),
	); err != nil {
		return errors.Wrap(err, "seek call failed")
	}
	return nil
}

func (c *sonosClient) Close() error {
	c.pollTicker.Stop()
	c.pollDoneChan <- true
	return nil
}
