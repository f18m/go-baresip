package gobaresip

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/markdingo/netstring"
)

// internalPingToken is a special token used for internal pings. Never use it in your code.
const internalPingToken = "gobaresip_internal_ping"

// ResponseMsg represents a response message from the baresip control interface.
// See doxygen docs at https://github.com/baresip/baresip/blob/main/modules/ctrl_tcp/ctrl_tcp.c
type ResponseMsg struct {
	Response bool   `json:"response,omitempty"`
	Ok       bool   `json:"ok,omitempty"`
	Data     string `json:"data,omitempty"`
	Token    string `json:"token,omitempty"`
	RawJSON  []byte `json:"-"`
}

// EventMsg represents an event message from the baresip control interface.
// See doxygen docs at https://github.com/baresip/baresip/blob/main/modules/ctrl_tcp/ctrl_tcp.c
type EventMsg struct {
	Event           bool   `json:"event,omitempty"`
	Type            string `json:"type,omitempty"`
	Class           string `json:"class,omitempty"`
	AccountAOR      string `json:"accountaor,omitempty"`
	Direction       string `json:"direction,omitempty"`
	PeerURI         string `json:"peeruri,omitempty"`
	PeerDisplayname string `json:"peerdisplayname,omitempty"`
	ID              string `json:"id,omitempty"`
	RemoteAudioDir  string `json:"remoteaudiodir,omitempty"`
	Param           string `json:"param,omitempty"`
	RawJSON         []byte `json:"-"`
}

// Logger is an interface that wraps the basic logging methods
// and can be used to bridge [Baresip] with a real logger implementation
// (e.g., logrus, zap, etc.).
type Logger interface {
	// Debug(args ...interface{}) // unused
	// Debugf(template string, args ...interface{}) // unused
	Info(args ...interface{})
	Infof(template string, args ...interface{})
}

// nopLogger is a no-op implementation of [Logger] (does nothing)
type nopLogger struct{}

// func (n *nopLogger) Debug(_ ...interface{})            {}
// func (n *nopLogger) Debugf(_ string, _ ...interface{}) {}
func (n *nopLogger) Info(_ ...interface{})            {}
func (n *nopLogger) Infof(_ string, _ ...interface{}) {}

// BareSipClientStats holds statistics about a [Baresip] instance.
type BareSipClientStats struct {
	TxStats struct {
		SuccessfulCmds  uint32 `json:"successful_cmds"`
		FailedCmds      uint32 `json:"failed_cmds"`
		SuccessfulPings uint32 `json:"successful_pings"`
		FailedPings     uint32 `json:"failed_pings"`
	}
	RxStats struct {
		DecodeFailures uint32 `json:"decode_failures"`
		EventMsgs      uint32 `json:"event_msg_count"`
		ResponseMsgs   uint32 `json:"response_msg_count"`
	}
}

// BaresipStartOptions holds the options for starting the internal baresip instance.
type BaresipStartOptions struct {
	// Name of the SIP user agent
	UserAgent string
	// Path to the baresip configuration directory. It defaults to the $HOME directory.
	ConfigPath string
	// Path to the audio files directory. It defaults to the current directory.
	AudioPath string
	// Debug mode. If true, it enables debug logging by baresip.
	Debug bool
}

// baresipInstanceController is an internal helper to manage a baresip instance.
type baresipInstanceController struct {
	startOptions BaresipStartOptions

	// logStdout and logStderr control whether to log baresip's stdout and stderr.
	// If true, the stdout/stderr of baresip will be logged to the logger set via the [SetLogger]
	// option passed to [New].
	// If false, the stdout/stderr will not be logged but will still be available via
	// [GetStdoutPipe] and [GetStderrPipe] methods.
	logStdout bool
	logStderr bool

	// baresipCmd is the exec.Cmd instance for running baresip.
	baresipCmd    *exec.Cmd
	baresipCtx    context.Context
	baresipCancel context.CancelFunc
	baresipStdout io.ReadCloser
	baresipStderr io.ReadCloser
}

func (b *baresipInstanceController) SetDefaults() {
	if b.startOptions.AudioPath == "" {
		b.startOptions.AudioPath = path.Join(os.Getenv("HOME"), ".baresip")
	}
	if b.startOptions.ConfigPath == "" {
		b.startOptions.ConfigPath = path.Join(os.Getenv("HOME"), ".baresip")
	}
	if b.startOptions.UserAgent == "" {
		b.startOptions.UserAgent = "go-baresip"
	}
}

// Baresip is the main struct for managing a baresip instance.
type Baresip struct {
	// BARESIP external process controller
	runBaresipCmd bool
	baresipHandle baresipInstanceController

	// OTHER CONFIGS

	// pingInterval is the interval for sending ping commands to baresip.
	pingInterval time.Duration

	// Timeout for writing commands to the control interface.
	ctrlCmdWriteTimeout time.Duration

	// TCP socket address for the control interface.
	ctrlAddr string

	// STATUS

	logger Logger

	// ctrlConn is the TCP connection to the baresip control interface.
	ctrlConn        net.Conn
	ctrlConnTxMutex sync.Mutex

	// decoder/encoder for netstring format
	ctrlConnDec *netstring.Decoder
	ctrlConnEnc *netstring.Encoder

	// stats
	ctrlStats BareSipClientStats

	// Channel of responses (to commands) coming from baresip TCP socket
	responseChan chan ResponseMsg

	// Channel of events (spontaneously sent by baresip) coming from baresip TCP socket
	eventChan chan EventMsg
}

// New creates a new [Baresip] instance with the provided options.
// Options can be set using functional options like [SetAudioPath], [SetBaresipDebug],
// [SetLogger], etc. If no options are provided, it will use default values.
func New(options ...func(*Baresip) error) (*Baresip, error) {
	b := &Baresip{
		responseChan:  make(chan ResponseMsg, 100),
		eventChan:     make(chan EventMsg, 100),
		runBaresipCmd: true,
	}

	if err := b.SetOption(options...); err != nil {
		return nil, err
	}

	b.baresipHandle.SetDefaults()

	if b.ctrlAddr == "" {
		// FIXME: read the config file instead and look for the ctrl_tcp_listen field
		b.ctrlAddr = "127.0.0.1:4444"
	}
	if b.logger == nil {
		b.logger = &nopLogger{} // Use a no-op logger if none is provided
	}
	if b.pingInterval == 0 {
		b.pingInterval = 30 * time.Second // Default ping interval
	}
	if b.ctrlCmdWriteTimeout == 0 {
		b.ctrlCmdWriteTimeout = 100 * time.Millisecond // Default write timeout for control commands
	}

	// FIXME:
	// Need to read the config file and check for
	//    module_app ctrl_tcp.so
	//    NO module stdio.so

	return b, nil
}

func (b *Baresip) readFromCtrlConn() error {
	for {
		netstr, err := b.ctrlConnDec.Decode()
		if err != nil {
			// network error, encoding error or end of stream... trigger baresip exit
			return fmt.Errorf("failure on the TCP control socket: %w", err)
		}

		// What we received might be only 2 types of messages:
		// 1. EventMsg
		// 2. ResponseMsg
		// We will try to unmarshal it as EventMsg first, and if that fails, we will try ResponseMsg.
		// If both fail, we will skip the message.
		var event EventMsg
		var response ResponseMsg
		// var isEvent, isResponse bool

		// Try unmarshalling first as EventMsg:
		err = json.Unmarshal(netstr, &event)
		if err != nil || !event.Event {

			// If unmarshalling as EventMsg fails, try unmarshalling as ResponseMsg
			err = json.Unmarshal(netstr, &response)
			if err != nil || !response.Response {
				// If both unmarshalling attempts fail, log the error and skip the message
				b.ctrlStats.RxStats.DecodeFailures++
				continue
			}

			// isResponse = true
			response.RawJSON = netstr
			b.onCtrlConnResponse(response)

		} else {
			// isEvent = true
			event.RawJSON = netstr
			b.onCtrlConnEvent(event)
		}
	}
}

func (b *Baresip) onCtrlConnEvent(event EventMsg) {
	b.ctrlStats.RxStats.EventMsgs++
	b.logger.Infof("event from baresip: %s", string(event.RawJSON))
	b.eventChan <- event
}

func (b *Baresip) onCtrlConnResponse(response ResponseMsg) {
	if response.Token == internalPingToken {
		// This is an internal ping response, hide that from the user (don't send on the response channel)
		b.ctrlStats.TxStats.SuccessfulPings++
		b.logger.Infof("ping successful, successful pings: %d", b.ctrlStats.TxStats.SuccessfulPings)
	} else {

		b.ctrlStats.RxStats.ResponseMsgs++
		b.logger.Infof("response from baresip: %s", string(response.RawJSON))
		b.responseChan <- response
	}
}

// GetEventChan returns the receive-only [EventMsg] channel for reading data.
func (b *Baresip) GetEventChan() <-chan EventMsg {
	return b.eventChan
}

// GetResponseChan returns the receive-only [ResponseMsg] channel for reading data.
func (b *Baresip) GetResponseChan() <-chan ResponseMsg {
	return b.responseChan
}

func (b *Baresip) keepActive(done <-chan bool) {
	if b.pingInterval <= 0 {
		b.logger.Info("pings / keep alives are disabled")
		return
	}

	ticker := time.NewTicker(b.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			b.logger.Infof("go-baresip goroutine stopped sending TCP keep-alives/pings")
			return

		case <-ticker.C:
			if b.Cmd("uuid", "", internalPingToken) != nil {
				// FIXME: shall we terminate the connection on a failed ping?
				b.ctrlStats.TxStats.FailedPings++
				b.logger.Infof("ping failed; count of failed pings: %d", b.ctrlStats.TxStats.FailedPings)
			}
		}
	}
}

// Serve method is the main method of [Baresip] and:
//   - runs an "internal" baresip instance, by executing it in background,
//     unless the [UseExternalBaresip] has been used in [New]
//   - connects to baresip control TCP socket; see [SetCtrlTCPAddr] for details
//
// This function will return if the provided context is cancelled.
// If an error occurs, it returns an error instead.
func (b *Baresip) Serve(ctx context.Context) error {
	// If needed start the internal baresip process
	if b.runBaresipCmd {
		if err := b.startInternalBaresip(); err != nil {
			return err
		}
	}

	// Connect to the control TCP socket
	if err := b.connectCtrl(); err != nil {
		return err
	}

	// Start reading from the control connection
	ctrlSocketErrCh := make(chan error, 1)
	go func() {
		err := b.readFromCtrlConn()
		ctrlSocketErrCh <- err // signal that the reading from the control socket has stopped
	}()

	// Run a continuous ping to detect if the baresip process is still alive
	stopKeepActiveCh := make(chan bool, 1)
	go b.keepActive(stopKeepActiveCh)

	// Block till either the context is cancelled or an error occurs on the control socket
	select {
	case <-ctx.Done():
		b.logger.Infof("context cancelled, stopping the Baresip instance")
		b.doCleanup(stopKeepActiveCh)
		return ctx.Err()

	case err := <-ctrlSocketErrCh:
		b.logger.Infof("error on control socket, stopping the Baresip instance")
		b.doCleanup(stopKeepActiveCh)
		return err
	}
}

func (b *Baresip) doCleanup(stopKeepActiveCh chan<- bool) {
	// Stop the keep-alive goroutine
	stopKeepActiveCh <- true

	// Close the control connection if it exists
	// This will also terminate the readFromCtrlConn() loop if it's still running
	if b.ctrlConn != nil {
		if err := b.ctrlConn.Close(); err != nil {
			b.logger.Infof("error closing control connection: %s", err)
		}
	}

	// Stop baresip instance, if any
	if b.runBaresipCmd {
		if err := b.baresipHandle.baresipCmd.Wait(); err != nil {
			b.logger.Infof("baresip exited with error: %s", err)
		}
	}

	// Close the stdout/stderr pipes if they exist
	if b.baresipHandle.baresipStdout != nil {
		_ = b.baresipHandle.baresipStdout.Close()
	}
	if b.baresipHandle.baresipStderr != nil {
		_ = b.baresipHandle.baresipStderr.Close()
	}

	// Clouse output channels
	close(b.responseChan)
	close(b.eventChan)
}

func (b *Baresip) startInternalBaresip() error {
	b.baresipHandle.baresipCtx, b.baresipHandle.baresipCancel = context.WithCancel(context.Background())

	// -c disables colored logs, which don't play well with our logic to redirect stdout/stderr to
	// the application's logger
	args := []string{
		"-f", b.baresipHandle.startOptions.ConfigPath,
		"-p", b.baresipHandle.startOptions.AudioPath,
		"-a", b.baresipHandle.startOptions.UserAgent,
		"-c",
	}
	if b.baresipHandle.startOptions.Debug {
		args = append(args, "-v")
	}

	// We assume baresip is in the PATH
	b.logger.Infof("Starting baresip with args: %v", args)
	b.baresipHandle.baresipCmd = exec.CommandContext(b.baresipHandle.baresipCtx, "baresip", args...) //nolint:gosec

	// Open stdout/stderr pipes BEFORE starting the command (this can't be done AFTER!)
	var err error
	b.baresipHandle.baresipStdout, err = b.baresipHandle.baresipCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error getting baresip stdout pipe: %w", err)
	}
	b.baresipHandle.baresipStderr, err = b.baresipHandle.baresipCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error getting baresip stderr pipe: %w", err)
	}

	// Start the baresip command
	if err := b.baresipHandle.baresipCmd.Start(); err != nil {
		return fmt.Errorf("error starting baresip: %w", err)
	}

	if b.baresipHandle.logStdout {
		go b.readOutput("stdout", b.baresipHandle.baresipStdout)
	}
	if b.baresipHandle.logStderr {
		go b.readOutput("stderr", b.baresipHandle.baresipStderr)
	}

	return nil
}

func (b *Baresip) connectCtrl() error {
	var err error

	attempts := 0
	for {
		b.logger.Infof("attempting to connect to control socket at %s (attempt %d)", b.ctrlAddr, attempts+1)
		b.ctrlConn, err = net.Dial("tcp", b.ctrlAddr)
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			attempts++
			if attempts < 10 {
				time.Sleep(100 * time.Millisecond) // Give baresip some time to start
			} else {
				// give up... too many attempts
				return ErrNoCtrlConn
			}

		case err != nil:
			// Some other error occurred, return it
			return fmt.Errorf("failed to connect to ctrl socket: please make sure ctrl_tcp baresip module is enabled: %w", err)
		}

		if err == nil {
			b.logger.Infof("successfully connected to control socket at %s", b.ctrlAddr)
			break // Successfully connected to the control socket
		}
	}

	// link the TCP socket to the netstring decoder/encoder
	b.ctrlConnDec = netstring.NewDecoder(b.ctrlConn)
	b.ctrlConnEnc = netstring.NewEncoder(b.ctrlConn)
	return nil
}

// GetStdoutPipe returns the stdout pipe of the baresip process.
// This can be used to read the stdout output of the baresip process directly.
// If logging is enabled via [SetOption] with [SetBaresipDebug] or
// [SetLogStdout], the output will also be logged to the logger set via [SetLogger].
func (b *Baresip) GetStdoutPipe() io.ReadCloser {
	return b.baresipHandle.baresipStdout
}

// GetStderrPipe returns the stderr pipe of the baresip process.
// See [GetStdoutPipe] for more details.
func (b *Baresip) GetStderrPipe() io.ReadCloser {
	return b.baresipHandle.baresipStderr
}

func (b *Baresip) readOutput(name string, reader io.ReadCloser) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		b.logger.Infof("baresip %s: %s", name, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		// FIXME: this might happen because the baresip command has been terminated or for something else;
		//         ideally we should have a way to report this to Baresip user (channel?)
		b.logger.Infof("go-baresip goroutine stopped reading %s due to error: %s", name, err)
		return
	}

	b.logger.Infof("go-baresip goroutine stopped reading %s", name)
}
