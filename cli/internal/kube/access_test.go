package kube

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

// TestForwardRejectsEmptySpecs covers the cheap guard that there is nothing to
// forward, before any cluster contact.
func TestForwardRejectsEmptySpecs(t *testing.T) {
	t.Parallel()

	_, err := (&Adapter{}).Forward(context.Background(), "ns", "pod", nil)
	require.EqualError(t, err, "no ports to forward")
}

// TestForwardRequiresClusterBackedClient covers the guard that port-forwarding
// needs the REST config/client the high-level (envtest) Adapter omits.
func TestForwardRequiresClusterBackedClient(t *testing.T) {
	t.Parallel()

	_, err := (&Adapter{}).Forward(
		context.Background(), "ns", "pod",
		[]PortForwardSpec{{Remote: 1337, Name: "ogmios"}},
	)
	require.EqualError(t, err, "port-forwarding requires a cluster-backed client")
}

// TestForwardHonorsContextCancellation proves Forward returns promptly when the
// context is cancelled mid-dial instead of hanging until the OS timeout. The
// real port-forward needs a live kubelet, but the cancellation path does not:
// a tarpit that accepts TCP yet never completes the TLS handshake stalls the
// SPDY dial, so neither readiness nor a start failure fires and only the cancel
// can unblock Forward. Before the fix, the ctx.Done arm called the blocking
// Close and this test would time out.
func TestForwardHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var connMu sync.Mutex
	var heldConns []net.Conn
	t.Cleanup(func() {
		_ = listener.Close()
		connMu.Lock()
		defer connMu.Unlock()
		for _, conn := range heldConns {
			_ = conn.Close()
		}
	})
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			// Hold the connection open (and referenced) without speaking TLS so
			// the client's handshake blocks for the duration of the test.
			connMu.Lock()
			heldConns = append(heldConns, conn)
			connMu.Unlock()
		}
	}()

	cfg := &rest.Config{
		Host:            "https://" + listener.Addr().String(),
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	adapter := &Adapter{restConfig: cfg, restClient: clientset.CoreV1().RESTClient()}

	type forwardResult struct {
		session ForwardSession
		err     error
	}
	results := make(chan forwardResult, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		session, forwardErr := adapter.Forward(ctx, "ns", "pod", []PortForwardSpec{{Remote: 1337, Name: "ogmios"}})
		results <- forwardResult{session: session, err: forwardErr}
	}()

	// Give the dial a moment to enter the stalled TLS handshake, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case res := <-results:
		assert.Nil(t, res.session, "no session is returned when the dial is cancelled")
		assert.Error(t, res.err, "cancellation surfaces as an error, not a nil success")
	case <-time.After(3 * time.Second):
		t.Fatal("Forward did not honor context cancellation promptly (hung on the dial)")
	}
}
