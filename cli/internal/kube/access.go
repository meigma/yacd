package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PortForwardSpec names a single primary-Pod container port to forward. Remote
// is the container port (which equals the published Service port for the
// chain-API endpoints); Name is the logical endpoint label used only in
// diagnostics.
type PortForwardSpec struct {
	Remote int32
	Name   string
}

// ForwardSession is a live set of port-forwards to one Pod. Callers read the
// assigned local ports, wait on Done for the forwards to stop (pod restart,
// dropped connection, or Close), and Close to tear them down. Err reports why
// forwarding stopped and is only meaningful once Done has fired.
type ForwardSession interface {
	// LocalPort returns the random local port assigned to the given remote
	// container port, and whether that remote port was forwarded.
	LocalPort(remote int32) (int, bool)

	// Done is closed when forwarding stops for any reason.
	Done() <-chan struct{}

	// Err returns the reason forwarding stopped; valid only after Done fires.
	// A clean Close reports nil.
	Err() error

	// Close tears the forwards down and blocks until they have stopped.
	Close() error
}

// ExecRequest carries an in-pod command invocation with kubectl-exec
// semantics. Command is an argv array executed directly (no shell); callers
// that need environment variables prepend the `env` binary to Command rather
// than relying on a shell, because PodExecOptions has no environment field.
type ExecRequest struct {
	Namespace string
	PodName   string
	Container string
	Command   []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	TTY       bool
}

// PrimaryPodName resolves the primary node Pod for a network by consuming the
// operator's published contract rather than its internal labels: it reads the
// node-to-node Service published in status, takes that Service's selector, and
// returns a Ready Pod matching it. The node-to-node Service always selects the
// single primary Pod that hosts every chain-API container, so it is the source
// of truth for which Pod the host-access verbs reach.
func (a *Adapter) PrimaryPodName(ctx context.Context, namespace string, networkName string) (string, error) {
	network, err := a.GetCardanoNetwork(ctx, namespace, networkName)
	if err != nil {
		return "", err
	}
	if network.Status.Endpoints == nil ||
		network.Status.Endpoints.NodeToNode == nil ||
		strings.TrimSpace(network.Status.Endpoints.NodeToNode.ServiceName) == "" {
		return "", fmt.Errorf("cardanonetwork %s/%s does not publish a primary node service yet", namespace, networkName)
	}
	serviceName := strings.TrimSpace(network.Status.Endpoints.NodeToNode.ServiceName)

	service := &corev1.Service{}
	if err := a.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceName}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("primary node service %s/%s %w", namespace, serviceName, ErrNotFound)
		}
		return "", fmt.Errorf("get primary node service %s/%s: %w", namespace, serviceName, err)
	}
	if len(service.Spec.Selector) == 0 {
		return "", fmt.Errorf("primary node service %s/%s has no selector", namespace, serviceName)
	}

	pods := &corev1.PodList{}
	if err := a.client.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels(service.Spec.Selector)); err != nil {
		return "", fmt.Errorf("list primary pods for %s/%s: %w", namespace, networkName, err)
	}
	for i := range pods.Items {
		if isPodReady(&pods.Items[i]) {
			return pods.Items[i].Name, nil
		}
	}

	return "", fmt.Errorf("cardanonetwork %s/%s has no ready primary pod (matched %d pods)", namespace, networkName, len(pods.Items))
}

// Forward establishes port-forwards from random local ports to the given Pod's
// container ports using client-go's SPDY port-forwarder (not a shelled-out
// kubectl). It blocks only until the forwards are ready, fail to start, or the
// context is cancelled, then returns a live ForwardSession whose assigned local
// ports are read through LocalPort.
func (a *Adapter) Forward(ctx context.Context, namespace string, podName string, specs []PortForwardSpec) (ForwardSession, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("no ports to forward")
	}
	if a.restConfig == nil || a.restClient == nil {
		return nil, fmt.Errorf("port-forwarding requires a cluster-backed client")
	}

	transport, upgrader, err := spdy.RoundTripperFor(a.restConfig)
	if err != nil {
		return nil, fmt.Errorf("build port-forward transport: %w", err)
	}
	requestURL := a.restClient.Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward").
		URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, requestURL)

	// A local port of 0 lets the kernel assign a free port, so parallel
	// networks never collide; GetPorts reports the assignment after readiness.
	ports := make([]string, 0, len(specs))
	for _, spec := range specs {
		ports = append(ports, fmt.Sprintf("0:%d", spec.Remote))
	}

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	// The forwarder writes per-connection chatter to out/errOut; the CLI
	// surfaces its own status, so both are discarded here.
	forwarder, err := portforward.New(dialer, ports, stopChan, readyChan, io.Discard, io.Discard)
	if err != nil {
		close(stopChan)
		return nil, fmt.Errorf("create port-forwarder for %s/%s: %w", namespace, podName, err)
	}

	session := &forwardSession{
		stopChan: stopChan,
		done:     make(chan struct{}),
		local:    make(map[int32]int, len(specs)),
	}
	go func() {
		// ForwardPorts blocks until stopChan is closed (clean) or a connection
		// is lost (error); recording err before closing done establishes the
		// happens-before that makes Err safe to read once Done fires.
		session.err = forwarder.ForwardPorts()
		close(session.done)
	}()

	select {
	case <-readyChan:
	case <-session.done:
		return nil, fmt.Errorf("port-forward to %s/%s failed to start: %w", namespace, podName, session.err)
	case <-ctx.Done():
		_ = session.Close()
		return nil, ctx.Err()
	}

	forwarded, err := forwarder.GetPorts()
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("resolve forwarded ports for %s/%s: %w", namespace, podName, err)
	}
	for _, port := range forwarded {
		session.local[int32(port.Remote)] = int(port.Local)
	}

	return session, nil
}

// Exec runs a command inside a Pod container with kubectl-exec semantics,
// streaming the caller's stdio. A non-zero remote exit surfaces as a
// k8s.io/client-go/util/exec ExitError carrying the exit code.
func (a *Adapter) Exec(ctx context.Context, req ExecRequest) error {
	if a.restConfig == nil || a.restClient == nil {
		return fmt.Errorf("exec requires a cluster-backed client")
	}
	if len(req.Command) == 0 {
		return fmt.Errorf("exec command is required")
	}

	requestURL := a.restClient.Post().
		Resource("pods").
		Namespace(req.Namespace).
		Name(req.PodName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: req.Container,
			Command:   req.Command,
			Stdin:     req.Stdin != nil,
			Stdout:    req.Stdout != nil,
			Stderr:    req.Stderr != nil,
			TTY:       req.TTY,
		}, scheme.ParameterCodec).
		URL()

	executor, err := remotecommand.NewSPDYExecutor(a.restConfig, http.MethodPost, requestURL)
	if err != nil {
		return fmt.Errorf("create executor for %s/%s: %w", req.Namespace, req.PodName, err)
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  req.Stdin,
		Stdout: req.Stdout,
		Stderr: req.Stderr,
		Tty:    req.TTY,
	})
}

// isPodReady reports whether a Pod is schedulable for host access: not being
// deleted and carrying a True PodReady condition.
func isPodReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}

// forwardSession is the Adapter's ForwardSession implementation. err is written
// once by the ForwardPorts goroutine before done is closed, so reads after Done
// (or after Close, which waits on done) are race-free.
type forwardSession struct {
	stopChan  chan struct{}
	done      chan struct{}
	local     map[int32]int
	err       error
	closeOnce sync.Once
}

func (s *forwardSession) LocalPort(remote int32) (int, bool) {
	local, ok := s.local[remote]

	return local, ok
}

func (s *forwardSession) Done() <-chan struct{} {
	return s.done
}

func (s *forwardSession) Err() error {
	return s.err
}

func (s *forwardSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopChan)
	})
	<-s.done

	return s.err
}
