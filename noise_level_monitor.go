package main

import (
	"log"
	"time"

	ma "github.com/RobinUS2/golang-moving-average"
	"github.com/pkg/errors"
	"gobot.io/x/gobot"

	"gobot.io/x/gobot/drivers/i2c"
	"gobot.io/x/gobot/platforms/raspi"
)

func StartNoiseLevelMonitor(samples int, samplingInterval time.Duration) (NoiseLevelMonitor, error) {
	monitor := &noiseLevelMonitor{
		samplingInterval: samplingInterval,
		average:          ma.Concurrent(ma.New(samples)),
	}

	// assume running on a Raspberry Pi with noise level monitor connected to I2C ADS1015 ADC
	rpiAdaptor := raspi.NewAdaptor()
	adc := i2c.NewADS1015Driver(rpiAdaptor)
	gain, err := adc.BestGainForVoltage(2.0)
	if err != nil {
		return nil, errors.Wrap(err, "failed to calculate best gain")
	}
	adc.DefaultGain = gain

	monitor.adcBot = gobot.NewRobot("ads1015bot",
		[]gobot.Connection{rpiAdaptor},
		[]gobot.Device{adc},
		func() {
			gobot.Every(samplingInterval, func() {
				v, err := adc.ReadDifferenceWithDefaults(0)
				if err != nil {
					// TODO(cdzombak): error handling from within the work loop could be improved
					log.Fatalf("failed to read ADC: %s", err)
				}
				db := v * 100
				monitor.average.Add(db)
			})
		},
	)
	if err := monitor.adcBot.Start(); err != nil {
		return nil, errors.Wrap(err, "failed to start ADC Gobot work loop")
	}

	return monitor, nil
}

type NoiseLevelMonitor interface {
	ReadAverage() float64
	Ready() bool
	Close() error
}

type noiseLevelMonitor struct {
	samplingInterval time.Duration
	average          *ma.ConcurrentMovingAverage
	adcBot           *gobot.Robot
}

func (m *noiseLevelMonitor) ReadAverage() float64 {
	return m.average.Avg()
}

func (m *noiseLevelMonitor) Ready() bool {
	return m.adcBot.Running() && m.average.SlotsFilled()
}

func (m *noiseLevelMonitor) Close() error {
	log.Println("stopping ADC Gobot work loop")
	return m.adcBot.Stop()
}
