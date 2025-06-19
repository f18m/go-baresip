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
	Debug(args ...interface{})
	Debugf(template string, args ...interface{})
	Info(args ...interface{})
	Infof(template string, args ...interface{})
}

// nopLogger is a no-op implementation of [Logger] (does nothing)
type nopLogger struct{}

func (n *nopLogger) Debug(_ ...interface{})            {}
func (n *nopLogger) Debugf(_ string, _ ...interface{}) {}
func (n *nopLogger) Info(_ ...interface{})             {}
func (n *nopLogger) Infof(_ string, _ ...interface{})  {}

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

// Baresip is the main struct for managing a baresip instance.
type Baresip struct {
	// OPTIONS AT BARESIP STARTUP

	// Name of the SIP user agent
	userAgent string
	// Path to the baresip configuration directory. It defaults to the $HOME directory.
	configPath string
	// Path to the audio files directory. It defaults to the current directory.
	audioPath string
	// Debug mode. If true, it enables debug logging by baresip.
	debug bool

	// logStdout and logStderr control whether to log baresip's stdout and stderr.
	// If true, the stdout/stderr of baresip will be logged to the logger set via the [SetLogger]
	// option passed to [New].
	// If false, the stdout/stderr will not be logged but will still be available via
	// [GetStdoutPipe] and [GetStderrPipe] methods.
	logStdout bool
	logStderr bool

	// OTHER CONFIGS

	// pingInterval is the interval for sending ping commands to baresip.
	pingInterval time.Duration

	// Timeout for writing commands to the control interface.
	ctrlCmdWriteTimeout time.Duration

	// TCP socket address for the control interface.
	ctrlAddr string

	// STATUS

	logger Logger

	// baresipCmd is the exec.Cmd instance for running baresip.
	baresipCmd    *exec.Cmd
	baresipCtx    context.Context
	baresipCancel context.CancelFunc
	baresipStdout io.ReadCloser
	baresipStderr io.ReadCloser

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
		responseChan: make(chan ResponseMsg, 100),
		eventChan:    make(chan EventMsg, 100),
	}

	if err := b.SetOption(options...); err != nil {
		return nil, err
	}

	if b.audioPath == "" {
		b.audioPath = "."
	}
	if b.configPath == "" {
		b.configPath = path.Join(os.Getenv("HOME"), ".baresip")
	}
	if b.ctrlAddr == "" {
		// FIXME: read the config file instead and look for the ctrl_tcp_listen field
		b.ctrlAddr = "127.0.0.1:4444"
	}
	if b.userAgent == "" {
		b.userAgent = "go-baresip"
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

func (b *Baresip) readFromCtrlConn() {
	for {

		netstr, err := b.ctrlConnDec.Decode()
		if err != nil {
			// network error, encoding error or end of stream... trigger baresip exit
			b.baresipCancel()
			break
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

	b.logger.Infof("go-baresip goroutine stopped reading TCP socket")
}

func (b *Baresip) onCtrlConnEvent(event EventMsg) {
	b.ctrlStats.RxStats.EventMsgs++
	b.logger.Infof("Event: %s", string(event.RawJSON))
	b.eventChan <- event
}

func (b *Baresip) onCtrlConnResponse(response ResponseMsg) {
	if response.Token == internalPingToken {
		// This is an internal ping response, hide that from the user (don't send on the response channel)
		b.ctrlStats.TxStats.SuccessfulPings++
		b.logger.Infof("Ping successful, successful pings: %d", b.ctrlStats.TxStats.SuccessfulPings)
	} else {

		b.ctrlStats.RxStats.ResponseMsgs++
		b.logger.Infof("Response: %s", string(response.RawJSON))
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

func (b *Baresip) keepActive() {
	if b.pingInterval <= 0 {
		b.logger.Info("Pings / keep alive is disabled")
		return
	}

	ticker := time.NewTicker(b.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.baresipCtx.Done():
			b.logger.Infof("go-baresip goroutine stopped sending TCP keep-alives/pings")
			return

		case <-ticker.C:
			if b.Cmd("uuid", "", internalPingToken) != nil {
				// FIXME: shall we terminate the connection on a failed ping?
				b.ctrlStats.TxStats.FailedPings++
				b.logger.Infof("Ping failed, failed pings: %d", b.ctrlStats.TxStats.FailedPings)
			}
		}
	}
}

// Start a baresip instance, by executing in background a baresip instance and then
// connecting to its control TCP socket.
// It returns a context.CancelFunc that can be used to stop the baresip instance.
// If an error occurs, it returns an error instead.
func (b *Baresip) Start() (context.CancelFunc, error) {
	b.baresipCtx, b.baresipCancel = context.WithCancel(context.Background())

	// -c disables colored logs, which don't play well with our logic to redirect stdout/stderr to
	// the application's logger
	args := []string{"-f", b.configPath, "-p", b.audioPath, "-a", b.userAgent, "-c"}
	if b.debug {
		args = append(args, "-v")
	}

	// We assume baresip is in the PATH
	b.logger.Infof("Starting baresip with args: %v", args)
	b.baresipCmd = exec.CommandContext(b.baresipCtx, "baresip", args...) //nolint:gosec

	// Open stdout/stderr pipes BEFORE starting the command (this can't be done AFTER!)
	var err error
	b.baresipStdout, err = b.baresipCmd.StdoutPipe()
	if err != nil {
		b.baresipCancel()
		return func() {}, fmt.Errorf("error getting baresip stdout pipe: %w", err)
	}
	b.baresipStderr, err = b.baresipCmd.StderrPipe()
	if err != nil {
		b.baresipCancel()
		return func() {}, fmt.Errorf("error getting baresip stderr pipe: %w", err)
	}

	// Start the baresip command
	if err := b.baresipCmd.Start(); err != nil {
		b.baresipCancel()
		return func() {}, fmt.Errorf("error starting baresip: %w", err)
	}

	if b.logStdout {
		go b.readOutput("stdout", b.baresipStdout)
	}
	if b.logStderr {
		go b.readOutput("stderr", b.baresipStderr)
	}

	// Connect to the control TCP socket
	if err := b.connectCtrl(); err != nil {
		b.baresipCancel()
		return func() {}, err
	}

	// Start reading from the control connection
	go b.readFromCtrlConn()

	// Simple solution for this https://github.com/baresip/baresip/issues/584
	go b.keepActive()

	return b.baresipCancel, nil
}

func (b *Baresip) connectCtrl() error {
	var err error

	attempts := 0
	for {
		b.ctrlConn, err = net.Dial("tcp", b.ctrlAddr)
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			attempts++
			if attempts < 10 {
				time.Sleep(100 * time.Millisecond) // Give baresip some time to start
			} else {
				// give up... too many attempts
				return err
			}

		case err != nil:
			// Some other error occurred, return it
			return fmt.Errorf("failed to connect to ctrl socket: please make sure ctrl_tcp baresip module is enabled: %w", err)
		}

		if err == nil {
			break // Successfully connected to the control socket
		}
	}

	// link the TCP socket to the netstring decoder/encoder
	b.ctrlConnDec = netstring.NewDecoder(b.ctrlConn)
	b.ctrlConnEnc = netstring.NewEncoder(b.ctrlConn)
	return nil
}

// WaitForShutdown has to be called after the [context.CancelFunc] returned by [Start] has been invoked.
// It will wait for the baresip external process to terminate and close all other resources associated with it.
func (b *Baresip) WaitForShutdown() error {
	if b.baresipCtx.Err() == nil {
		// this is a logical programming error...
		return fmt.Errorf("Baresip is still running, please cancel its context provided by Start() before calling WaitForShutdown()")
	}

	// Wait for the baresip command to finish
	if b.baresipCmd != nil {
		if err := b.baresipCmd.Wait(); err != nil {
			return fmt.Errorf("baresip exited with error: %w", err)
		}
	}

	// Close the control connection if it exists
	if b.ctrlConn != nil {
		if err := b.ctrlConn.Close(); err != nil {
			return fmt.Errorf("error closing control connection: %w", err)
		}
	}

	close(b.responseChan)
	close(b.eventChan)
	return nil
}

// GetStdoutPipe returns the stdout pipe of the baresip process.
// This can be used to read the stdout output of the baresip process directly.
// If logging is enabled via [SetOption] with [SetBaresipDebug] or
// [SetLogStdout], the output will also be logged to the logger set via [SetLogger].
func (b *Baresip) GetStdoutPipe() io.ReadCloser {
	return b.baresipStdout
}

// GetStderrPipe returns the stderr pipe of the baresip process.
// See [GetStdoutPipe] for more details.
func (b *Baresip) GetStderrPipe() io.ReadCloser {
	return b.baresipStderr
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
