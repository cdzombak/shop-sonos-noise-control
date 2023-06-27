package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/ianr0bkny/go-sonos"
	"github.com/ianr0bkny/go-sonos/ssdp"
	"github.com/ianr0bkny/go-sonos/upnp"
	"github.com/pkg/errors"
)

func runSonosTest(iface, targetSonosId string, verbose bool) error {
	mgr := ssdp.MakeManager()
	defer func(mgr ssdp.Manager) {
		if err := mgr.Close(); err != nil {
			log.Printf("failed to close SSDP manager: %s\n", err)
		}
	}(mgr)

	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)
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
	sonosClient := sonos.Connect(sonosDevice, nil, sonos.SVC_AV_TRANSPORT)

	info, err := sonosClient.GetTransportInfo(0)
	if err != nil {
		return errors.Wrap(err, "GetTransportInfo call failed")
	}
	fmt.Printf("Sonos device playback state: %s\n", info.CurrentTransportState)
	if info.CurrentTransportState == upnp.State_PLAYING {
		fmt.Println("Pausing playback for 3 seconds...")
		if err := sonosClient.Pause(0); err != nil {
			return errors.Wrap(err, "Pause call failed")
		}
		time.Sleep(2 * time.Second)
		fmt.Println("Seeking back 10 seconds...")
		if err := sonosClient.Seek(0, "TIME_DELTA", "-00:00:10"); err != nil {
			return errors.Wrap(err, "Seek call failed")
		}
		fmt.Println("Resuming playback...")
		if err := sonosClient.Play(0, info.CurrentSpeed); err != nil {
			return errors.Wrap(err, "Play call failed")
		}
	}

	return nil
}
