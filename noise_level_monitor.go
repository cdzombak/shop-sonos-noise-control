package main

import (
	"fmt"
	"log"
	"time"

	ma "github.com/RobinUS2/golang-moving-average"
	"github.com/pkg/errors"
	"gobot.io/x/gobot"

	"gobot.io/x/gobot/drivers/i2c"
	"gobot.io/x/gobot/platforms/raspi"
)

func StartNoiseLevelMonitor(samples int, samplingInterval time.Duration, minDb, maxDb float64, escalator AsyncErrorEscalator) (NoiseLevelMonitor, error) {
	monitor := &noiseLevelMonitor{
		samplingInterval: samplingInterval,
		average:          ma.Concurrent(ma.New(samples)),
		minDb:            minDb,
		maxDb:            maxDb,
	}

	// assume running on a Raspberry Pi with noise level monitor connected to I2C ADS1015 ADC
	rpiAdaptor := raspi.NewAdaptor()
	adc := i2c.NewADS1015Driver(rpiAdaptor)
	gain, err := adc.BestGainForVoltage(2.0)
	if err != nil {
		return nil, errors.Wrap(err, "failed to calculate best gain")
	}
	adc.DefaultGain = gain

	adcReadFailureChan := escalator.RegisterPolicy(&ErrorCountThresholdPolicy{
		ErrorCount: 60,
		TimeWindow: 30 * time.Second,
		Name:       "ADC read failure rate >= 50%",
		Log:        true,
	})

	monitor.adcBot = gobot.NewRobot("ads1015bot",
		[]gobot.Connection{rpiAdaptor},
		[]gobot.Device{adc},
		func() {
			gobot.Every(samplingInterval, func() {
				v, err := adc.ReadDifferenceWithDefaults(0)
				if err != nil {
					adcReadFailureChan <- fmt.Errorf("failed to read ADC: %w", err)
					return
				}
				db := v * 100
				if minDb < db && db < maxDb {
					monitor.average.Add(db)
				} else {
					adcReadFailureChan <- fmt.Errorf("read ADC value out of dB filter range: %f", db)
					return
				}
			})
		},
	)
	go func() {
		if err := monitor.adcBot.Start(); err != nil {
			adcStartFailureChan := escalator.RegisterPolicy(&ImmediateEscalationPolicy{Name: "ADC Gobot failure"})
			adcStartFailureChan <- fmt.Errorf("ADC Gobot work loop failed: %w", err)
		}
	}()

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
	minDb, maxDb     float64
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
