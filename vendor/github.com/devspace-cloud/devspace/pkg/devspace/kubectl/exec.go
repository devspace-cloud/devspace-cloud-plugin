package kubectl

import (
	"bytes"
	"io"
	"net/http"

	"github.com/devspace-cloud/devspace/pkg/util/terminal"
	corev1 "k8s.io/api/core/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	kubectlExec "k8s.io/client-go/util/exec"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/term"
)

// SubResource specifies with sub resources should be used for the container connection (exec or attach)
type SubResource string

const (
	// SubResourceExec creates a new process in the container and attaches to that
	SubResourceExec SubResource = "exec"

	// SubResourceAttach attaches to the top process of the container
	SubResourceAttach SubResource = "attach"
)

// ExecStreamWithTransportOptions are the options used for executing a stream
type ExecStreamWithTransportOptions struct {
	ExecStreamOptions

	Transport   http.RoundTripper
	Upgrader    spdy.Upgrader
	SubResource SubResource
}

// ExecStreamWithTransport executes a kubectl exec with given transport round tripper and upgrader
func (client *client) ExecStreamWithTransport(options *ExecStreamWithTransportOptions) error {
	var (
		t             term.TTY
		sizeQueue     remotecommand.TerminalSizeQueue
		streamOptions remotecommand.StreamOptions
		tty           = options.TTY
	)

	execRequest := client.KubeClient().CoreV1().RESTClient().Post().
		Resource("pods").
		Name(options.Pod.Name).
		Namespace(options.Pod.Namespace).
		SubResource(string(options.SubResource))

	if tty {
		tty, t = terminal.SetupTTY(options.Stdin, options.Stdout)
		if options.ForceTTY || tty {
			tty = true
			if t.Raw {
				// this call spawns a goroutine to monitor/update the terminal size
				sizeQueue = t.MonitorSize(t.GetSize())
			}

			streamOptions = remotecommand.StreamOptions{
				Stdin:             t.In,
				Stdout:            t.Out,
				Stderr:            options.Stderr,
				Tty:               t.Raw,
				TerminalSizeQueue: sizeQueue,
			}
		}
	}
	if !tty {
		streamOptions = remotecommand.StreamOptions{
			Stdin:  options.Stdin,
			Stdout: options.Stdout,
			Stderr: options.Stderr,
		}
	}

	if options.SubResource == SubResourceExec {
		execRequest.VersionedParams(&corev1.PodExecOptions{
			Container: options.Container,
			Command:   options.Command,
			Stdin:     options.Stdin != nil,
			Stdout:    options.Stdout != nil,
			Stderr:    options.Stderr != nil,
			TTY:       tty,
		}, scheme.ParameterCodec)
	} else if options.SubResource == SubResourceAttach {
		execRequest.VersionedParams(&corev1.PodExecOptions{
			Container: options.Container,
			Stdin:     options.Stdin != nil,
			Stdout:    options.Stdout != nil,
			Stderr:    options.Stderr != nil,
			TTY:       tty,
		}, scheme.ParameterCodec)
	}

	exec, err := remotecommand.NewSPDYExecutorForTransports(options.Transport, options.Upgrader, "POST", execRequest.URL())
	if err != nil {
		return err
	}

	return t.Safe(func() error {
		return exec.Stream(streamOptions)
	})
}

// ExecStreamOptions are the options for ExecStream
type ExecStreamOptions struct {
	Pod *k8sv1.Pod

	Container string
	Command   []string

	ForceTTY bool
	TTY      bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// ExecStream executes a command and streams the output to the given streams
func (client *client) ExecStream(options *ExecStreamOptions) error {
	wrapper, upgradeRoundTripper, err := client.GetUpgraderWrapper()
	if err != nil {
		return err
	}

	return client.ExecStreamWithTransport(&ExecStreamWithTransportOptions{
		ExecStreamOptions: *options,
		Transport:         wrapper,
		Upgrader:          upgradeRoundTripper,
		SubResource:       SubResourceExec,
	})
}

// ExecBuffered executes a command for kubernetes and returns the output and error buffers
func (client *client) ExecBuffered(pod *k8sv1.Pod, container string, command []string, stdin io.Reader) ([]byte, []byte, error) {
	stdoutBuffer := &bytes.Buffer{}
	stderrBuffer := &bytes.Buffer{}

	kubeExecError := client.ExecStream(&ExecStreamOptions{
		Pod:       pod,
		Container: container,
		Command:   command,
		Stdin:     stdin,
		Stdout:    stdoutBuffer,
		Stderr:    stderrBuffer,
	})
	if kubeExecError != nil {
		if _, ok := kubeExecError.(kubectlExec.CodeExitError); ok == false {
			return nil, nil, kubeExecError
		}
	}

	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), kubeExecError
}
