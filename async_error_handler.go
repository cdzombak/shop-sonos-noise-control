package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type AsyncErrorPolicy interface {
	Close()
	GetDesiredBufferSize() int
	GetName() string
	Receive(err error) bool
}

func NewAsyncErrorEscalator() AsyncErrorEscalator {
	return &asyncErrorEscalator{
		errEscalationChan: make(chan error),
	}
}

type AsyncErrorEscalator interface {
	EscalationChannel() chan error
	RegisterPolicy(policy AsyncErrorPolicy) chan error
	UnregisterPolicy(policy AsyncErrorPolicy)
}

type asyncErrorEscalator struct {
	errEscalationChan chan error
	policiesMutex     sync.Mutex
	policies          map[AsyncErrorPolicy]policyRecord
}

func (h *asyncErrorEscalator) EscalationChannel() chan error {
	return h.errEscalationChan
}

const (
	defaultErrorChanBufferSize = 100
)

type policyRecord struct {
	errorChannel chan error
	closer       func()
}

func (h *asyncErrorEscalator) RegisterPolicy(policy AsyncErrorPolicy) chan error {
	h.policiesMutex.Lock()
	defer h.policiesMutex.Unlock()

	if h.policies == nil {
		h.policies = make(map[AsyncErrorPolicy]policyRecord)
	}
	if _, ok := h.policies[policy]; ok {
		panic("policy is already registered")
	}

	bufSize := policy.GetDesiredBufferSize()
	if bufSize <= 0 {
		bufSize = defaultErrorChanBufferSize
	}
	errorChan := make(chan error, bufSize)
	closerChan := make(chan bool)

	go func() {
		for {
			select {
			case err := <-errorChan:
				if policy.Receive(err) {
					name := policy.GetName()
					if name == "" {
						name = "<unnamed>"
					}
					h.errEscalationChan <- fmt.Errorf("async error policy '%s' escalated: %w", name, err)
				}
			case <-closerChan:
				return
			}
		}
	}()

	h.policies[policy] = policyRecord{
		errorChannel: errorChan,
		closer: func() {
			policy.Close()
			closerChan <- true
			close(closerChan)
			close(errorChan)
		},
	}

	return errorChan
}

func (h *asyncErrorEscalator) UnregisterPolicy(policy AsyncErrorPolicy) {
	h.policiesMutex.Lock()
	defer h.policiesMutex.Unlock()

	if _, ok := h.policies[policy]; !ok {
		panic("policy is not registered")
	}

	h.policies[policy].closer()
	delete(h.policies, policy)
}

type errorTimeRecord struct {
	At  time.Time
	Err error
}

// ErrorCountThresholdPolicy will terminate the program if more than ErrorCount errors
// are received within TimeWindow.
type ErrorCountThresholdPolicy struct {
	ErrorCount int
	TimeWindow time.Duration
	Name       string
	Log        bool

	lastCompression time.Time
	errors          []errorTimeRecord
	mutex           sync.Mutex
}

func (e *ErrorCountThresholdPolicy) Close()                    {}
func (e *ErrorCountThresholdPolicy) GetDesiredBufferSize() int { return 100 }
func (e *ErrorCountThresholdPolicy) GetName() string           { return e.Name }

func (e *ErrorCountThresholdPolicy) Receive(err error) bool {
	if e.Log {
		go log.Println(err.Error())
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := time.Now()
	e.errors = append(e.errors, errorTimeRecord{
		At:  now,
		Err: err,
	})

	errorsInWindow := 0
	for _, errRecord := range e.errors {
		if now.Sub(errRecord.At) <= e.TimeWindow {
			errorsInWindow++
		}
	}

	if errorsInWindow >= e.ErrorCount {
		return true
	}

	if now.Sub(e.lastCompression) >= e.TimeWindow {
		e.lastCompression = now
		newErrors := make([]errorTimeRecord, 0, len(e.errors))
		for _, errRecord := range e.errors {
			if now.Sub(errRecord.At) <= e.TimeWindow {
				newErrors = append(newErrors, errRecord)
			}
		}
		e.errors = newErrors
	}

	return false
}

type ImmediateEscalationPolicy struct {
	Name string
}

func (i *ImmediateEscalationPolicy) Close()                    {}
func (i *ImmediateEscalationPolicy) GetDesiredBufferSize() int { return 1 }
func (i *ImmediateEscalationPolicy) GetName() string           { return i.Name }
func (i *ImmediateEscalationPolicy) Receive(_ error) bool      { return true }
