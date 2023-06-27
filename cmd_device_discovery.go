package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/ianr0bkny/go-sonos/ssdp"
	"github.com/pkg/errors"
)

func runDeviceDiscovery(iface string, verbose bool) error {
	mgr := ssdp.MakeManager()
	defer func(mgr ssdp.Manager) {
		if err := mgr.Close(); err != nil {
			log.Printf("failed to close SSDP manager: %s\n", err)
		}
	}(mgr)

	// Discover()
	//  eth0 := Network device to query for UPnP devices
	// 11209 := Free local port for discovery replies
	// false := Do not subscribe for asynchronous updates
	if !verbose {
		log.SetOutput(io.Discard)
		defer log.SetOutput(os.Stderr)
	}
	if err := mgr.Discover(iface, "11209", false); err != nil {
		return errors.Wrapf(err, "SSDP discovery on interface %s failed", iface)
	}

	log.SetOutput(os.Stderr)
	// ServiceQueryTerms
	// A map of service keys to minimum required version
	qry := ssdp.ServiceQueryTerms{
		ssdp.ServiceKey("schemas-upnp-org-MusicServices"): -1,
	}

	// Look for the service keys in qry in the database of discovered devices
	result := mgr.QueryServices(qry)
	if devices, ok := result["schemas-upnp-org-MusicServices"]; ok {
		for _, device := range devices {
			fmt.Printf(
				"%s  %s  %s  %s  %s\n",
				device.Product(),
				device.ProductVersion(),
				device.Name(),
				device.Location(),
				device.UUID(),
			)
		}
	}

	return nil
}
