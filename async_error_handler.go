package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// TODO(cdzombak): docs and usage examples
// TODO(cdzombak): unidirectional channels

const (
	defaultErrChanBufferSize = 32
)

// AsyncErrorPolicy describes a policy for handling errors that occur in an asynchronous context.
type AsyncErrorPolicy interface {
	Close()
	GetDesiredBufferSize() int
	GetName() string
	GetUniqID() string
	Receive(err error) bool
}

// NewAsyncErrorEscalator returns a new AsyncErrorEscalator.
func NewAsyncErrorEscalator() AsyncErrorEscalator {
	return &asyncErrorEscalator{
		errEscalationChan: make(chan error, defaultErrChanBufferSize),
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
	policies          map[string]policyRecord
}

func (h *asyncErrorEscalator) EscalationChannel() chan error {
	return h.errEscalationChan
}

type policyRecord struct {
	closer func()
	uid    string
}

func uidForPolicy(policy AsyncErrorPolicy) string {
	if policy.GetUniqID() != "" {
		return policy.GetUniqID()
	}
	if policy.GetName() != "" {
		return policy.GetName()
	}
	panic(fmt.Sprintf("policy has no Name or UniqID: %v", policy))
}

func (h *asyncErrorEscalator) RegisterPolicy(policy AsyncErrorPolicy) chan error {
	h.policiesMutex.Lock()
	defer h.policiesMutex.Unlock()

	if h.policies == nil {
		h.policies = make(map[string]policyRecord)
	}
	policyUid := uidForPolicy(policy)
	if _, ok := h.policies[policyUid]; ok {
		panic(fmt.Sprintf("policy '%s' is already registered", policyUid))
	}

	bufSize := policy.GetDesiredBufferSize()
	if bufSize <= 0 {
		bufSize = defaultErrChanBufferSize
	}
	errorChan := make(chan error, bufSize)
	closeChan := make(chan struct{})

	go func() {
		for {
			select {
			case err := <-errorChan:
				go func() {
					name := policy.GetName()
					uid := policy.GetUniqID()
					if name == "" {
						name = "<unnamed>"
					}
					if policy.Receive(err) {
						if uid != "" {
							h.errEscalationChan <- fmt.Errorf("async error policy '%s' (%s) escalated: %w", name, uid, err)
						} else {
							h.errEscalationChan <- fmt.Errorf("async error policy '%s' escalated: %w", name, err)
						}
					}
				}()
			case <-closeChan:
				return
			}
		}
	}()

	h.policies[policyUid] = policyRecord{
		uid: policyUid,
		closer: func() {
			close(errorChan)
			policy.Close()
			close(closeChan)
		},
	}

	return errorChan
}

func (h *asyncErrorEscalator) UnregisterPolicy(policy AsyncErrorPolicy) {
	h.policiesMutex.Lock()
	defer h.policiesMutex.Unlock()

	policyUid := uidForPolicy(policy)
	if _, ok := h.policies[policyUid]; !ok {
		panic(fmt.Sprintf("policy '%s' is not registered", policyUid))
	}

	h.policies[policyUid].closer()
	delete(h.policies, policyUid)
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
	UniqID     string
	LogEvery   int

	lastCompression     time.Time
	skippedSinceLastLog int
	errors              []errorTimeRecord
	mutex               sync.Mutex
}

func (e *ErrorCountThresholdPolicy) Close()                    {}
func (e *ErrorCountThresholdPolicy) GetDesiredBufferSize() int { return e.ErrorCount * 2 }
func (e *ErrorCountThresholdPolicy) GetName() string           { return e.Name }
func (e *ErrorCountThresholdPolicy) GetUniqID() string         { return e.UniqID }

func (e *ErrorCountThresholdPolicy) Receive(err error) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.LogEvery > 0 {
		e.skippedSinceLastLog++
		if e.skippedSinceLastLog >= e.LogEvery {
			go log.Println(err.Error())
			e.skippedSinceLastLog = 0
		}
	}

	now := time.Now()
	errorsInWindow := 0
	performCompress := now.Sub(e.lastCompression) > e.TimeWindow
	var compressedErrors []errorTimeRecord

	e.errors = append(e.errors, errorTimeRecord{
		At:  now,
		Err: err,
	})

	if performCompress {
		newSliceSize := len(e.errors) / 2
		if newSliceSize <= 1 {
			newSliceSize = 2
		}
		compressedErrors = make([]errorTimeRecord, 0, newSliceSize)
	}

	for _, errRecord := range e.errors {
		if now.Sub(errRecord.At) <= e.TimeWindow {
			errorsInWindow++

			if performCompress {
				compressedErrors = append(compressedErrors, errRecord)
			}
		}
	}

	if performCompress {
		e.lastCompression = now
		e.errors = compressedErrors
	}

	return errorsInWindow >= e.ErrorCount
}

type ImmediateEscalationPolicy struct {
	Name   string
	UniqID string
	Log    bool
}

func (i *ImmediateEscalationPolicy) Close()                    {}
func (i *ImmediateEscalationPolicy) GetDesiredBufferSize() int { return 1 }
func (i *ImmediateEscalationPolicy) GetName() string           { return i.Name }
func (i *ImmediateEscalationPolicy) GetUniqID() string         { return i.UniqID }

func (i *ImmediateEscalationPolicy) Receive(err error) bool {
	if i.Log {
		log.Println(err.Error())
	}
	return true
}
