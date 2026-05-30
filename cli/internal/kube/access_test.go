package kube

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestForwardSessionLifecycle locks the forwardSession contract that Forward
// relies on: LocalPort resolves forwarded remotes, Close stops the forwards and
// returns the stop reason recorded by the (here simulated) ForwardPorts
// goroutine, Done is observable, Err is stable afterwards, and Close is
// idempotent. The real port-forward needs a live kubelet, so the session
// machinery is exercised here without one.
func TestForwardSessionLifecycle(t *testing.T) {
	t.Parallel()

	session := &forwardSession{
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
		local:    map[int32]int{1337: 40001, 1442: 40002},
	}

	local, ok := session.LocalPort(1337)
	assert.True(t, ok)
	assert.Equal(t, 40001, local)
	_, ok = session.LocalPort(9999)
	assert.False(t, ok, "an unforwarded remote port has no local mapping")

	// Stand in for the ForwardPorts goroutine: record the stop reason, then
	// close done. The ordering mirrors Forward so the happens-before that makes
	// Err safe to read after Done is exercised.
	go func() {
		<-session.stopChan
		session.err = errors.New("lost connection to pod")
		close(session.done)
	}()

	assert.EqualError(t, session.Close(), "lost connection to pod")
	select {
	case <-session.Done():
	default:
		t.Fatal("Done channel was not closed after Close")
	}
	assert.EqualError(t, session.Err(), "lost connection to pod")

	// Close is idempotent: a second call must not re-close stopChan (panic) and
	// returns the same reason.
	assert.EqualError(t, session.Close(), "lost connection to pod")
}
