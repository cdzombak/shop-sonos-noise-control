package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAsyncErrorEscalator_PolicyMgmt(t *testing.T) {
	sut := NewAsyncErrorEscalator()
	require.NotNil(t, sut)

	policy := &ImmediateEscalationPolicy{
		Name: "test policy",
	}
	var policyErrChan chan error

	require.NotPanics(t, func() {
		policyErrChan = sut.RegisterPolicy(policy)
	})
	require.Panics(t, func() {
		_ = sut.RegisterPolicy(policy)
	})

	require.NotNil(t, policyErrChan)
	require.Equal(t, policy.GetDesiredBufferSize(), cap(policyErrChan))
	require.Equal(t, 0, len(policyErrChan))

	require.NotPanics(t, func() {
		policyErrChan <- makeError()
	})

	require.NotPanics(t, func() {
		sut.UnregisterPolicy(policy)
	})
	require.Panics(t, func() {
		sut.UnregisterPolicy(policy)
	})
	require.Panics(t, func() {
		policyErrChan <- makeError()
	})
}

func TestAsyncErrorEscalator_Escalation(t *testing.T) {
	sut := NewAsyncErrorEscalator()
	require.NotNil(t, sut)

	const policyTimeWindow = 2 * time.Second
	const policyErrCount = 10
	policy := &ErrorCountThresholdPolicy{
		Name:       "test policy",
		ErrorCount: policyErrCount,
		TimeWindow: policyTimeWindow,
	}
	policyErrChan := sut.RegisterPolicy(policy)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		timeout := time.NewTimer(policyTimeWindow)
		select {
		case <-sut.EscalationChannel():
			wg.Done()
		case <-timeout.C:
			t.Error("escalation channel didn't receive any error within the policy time window")
			wg.Done()
		}
	}()
	for i := 0; i < policyErrCount; i++ {
		wg.Add(1)
		go func() {
			policyErrChan <- makeError()
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestErrorCountThresholdPolicy(t *testing.T) {
	const policyTimeWindow = 1 * time.Second
	sut := &ErrorCountThresholdPolicy{
		ErrorCount: 2,
		TimeWindow: policyTimeWindow,
	}

	// verify Receive logic:
	require.False(t, sut.Receive(makeError()))
	require.True(t, sut.Receive(makeError()))
	require.True(t, sut.Receive(makeError()))
	// verify compression logic:
	require.Equal(t, 3, len(sut.errors))
	require.GreaterOrEqual(t, cap(sut.errors), 3)

	time.Sleep(policyTimeWindow)
	// verify Receive logic:
	require.False(t, sut.Receive(makeError()))
	// verify compression logic:
	require.Equal(t, 1, len(sut.errors))
	require.Less(t, cap(sut.errors), 3)

	time.Sleep(policyTimeWindow)
	// verify Receive logic:
	require.False(t, sut.Receive(makeError()))
	require.True(t, sut.Receive(makeError()))
	// verify compression logic:
	require.Equal(t, 2, len(sut.errors))
	require.Less(t, cap(sut.errors), 3)
}

func TestErrorCountThresholdPolicy_Stress(t *testing.T) {
	const policyTimeWindow = 5 * time.Second
	sut := &ErrorCountThresholdPolicy{
		ErrorCount: 10000,
		TimeWindow: policyTimeWindow,
	}

	wg := sync.WaitGroup{}
	for i := 0; i < 9; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < 1000; i++ {
				require.False(t, sut.Receive(makeError()))
			}
			wg.Done()
		}()
	}
	wg.Add(1)
	go func() {
		for i := 0; i < 999; i++ {
			require.False(t, sut.Receive(makeError()))
		}
		wg.Done()
	}()
	wg.Wait()

	require.True(t, sut.Receive(makeError()))

	time.Sleep(policyTimeWindow)
	require.Equal(t, 10000, len(sut.errors))
	require.GreaterOrEqual(t, cap(sut.errors), 10000)
	require.False(t, sut.Receive(makeError()))
	require.Equal(t, 1, len(sut.errors))
	require.Less(t, cap(sut.errors), 10000)
}

func TestImmediateEscalationPolicy_Receive(t *testing.T) {
	sut := &ImmediateEscalationPolicy{}
	require.True(t,
		sut.Receive(makeError()),
		"ImmediateEscalationPolicy.Receive() should always return true",
	)
}

var errCount = atomic.Int64{}

func makeError() error {
	return fmt.Errorf(
		"error %d: %s",
		errCount.Add(1),
		time.Now().Format(time.RFC3339Nano),
	)
}
