package gobaresip

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"github.com/markdingo/netstring"
)

// ResponseMsg
type ResponseMsg struct {
	Response bool   `json:"response,omitempty"`
	Ok       bool   `json:"ok,omitempty"`
	Data     string `json:"data,omitempty"`
	Token    string `json:"token,omitempty"`
	RawJSON  []byte `json:"-"`
}

// EventMsg
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

// Logger is the interface that wraps the basic logging methods
type Logger interface {
	Debug(args ...interface{})
	Debugf(template string, args ...interface{})
	Info(args ...interface{})
	Infof(template string, args ...interface{})
}

// nopLogger is a no-op implementation of Logger (does nothing)
type nopLogger struct{}

func (n *nopLogger) Debug(_ ...interface{})            {}
func (n *nopLogger) Debugf(_ string, _ ...interface{}) {}
func (n *nopLogger) Info(_ ...interface{})             {}
func (n *nopLogger) Infof(_ string, _ ...interface{})  {}

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

	// OTHER CONFIGS

	// TCP socket address for the control interface.
	ctrlAddr string

	// WebSocket address for the control interface (optional)
	wsAddr string

	// STATUS

	logger Logger

	// baresipCmd is the exec.Cmd instance for running baresip.
	baresipCmd    *exec.Cmd
	baresipCtx    context.Context
	baresipCancel context.CancelFunc

	// ctrlConn is the TCP connection to the baresip control interface.
	ctrlConn        net.Conn
	ctrlConnTxMutex sync.Mutex

	// decoder/encoder for netstring format
	ctrlConnDec *netstring.Decoder
	ctrlConnEnc *netstring.Encoder

	// stats
	successfulCmds  uint32 // Number of successful commands sent to the baresip control interface
	failedCmds      uint32 // Number of failed commands sent to the baresip control interface
	successfulPings uint32 // Number of successful pings to the baresip control interface
	failedPings     uint32 // Number of failed pings to the baresip control interface

	// Channel of responses (to commands) coming from baresip TCP socket
	responseChan chan ResponseMsg

	// Channel of events (spontaneously sent by baresip) coming from baresip TCP socket
	eventChan chan EventMsg

	// WebSocket channels for responses and events (if WebSocket support is enabled)
	responseWsChan chan []byte
	eventWsChan    chan []byte

	autoCmd ac
}

type ac struct {
	mux sync.RWMutex
	num map[string]int

	hangupGap uint32
}

// ping is a netstring-encoded message that is a "special" command having just the "token"
// and no "command" and "param" fields; compare with CommandMsg.
var ping = []byte(`16:{"token":"ping"},`)

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

	b.autoCmd.num = make(map[string]int)

	// FIXME:
	// Need to read the config file and check for
	//    module_app ctrl_tcp.so
	//    NO module stdio.so

	/* WEBSOCKET SUPPORT DISABLED
	if b.wsAddr != "" {
		b.responseWsChan = make(chan []byte, 100)
		b.eventWsChan = make(chan []byte, 100)

		h := newWsHub(b)
		go h.run()

		http.HandleFunc("/", serveRoot)
		http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			serveWs(h, w, r)
		})
		go http.ListenAndServe(b.wsAddr, nil)
	}*/

	return b, nil
}

func (b *Baresip) read() {
	for {

		netstr, err := b.ctrlConnDec.Decode()
		if err != nil {
			// network error, encoding error or end of stream... trigger baresip exit
			b.baresipCancel()
			break
		}

		fmt.Printf("Received: %s\n", netstr)
		/*
			if bytes.Contains(msg, []byte("\"event\":true")) {
				if bytes.Contains(msg, []byte(",end of file")) {
					msg = bytes.Replace(msg, []byte("AUDIO_ERROR"), []byte("AUDIO_EOF"), 1)
				}

				var e EventMsg
				e.RawJSON = msg

				err := json.Unmarshal(e.RawJSON, &e)
				if err != nil {
					log.Println(err, string(e.RawJSON))
					continue
				}

				b.eventChan <- e
				if b.wsAddr != "" {
					select {
					case b.eventWsChan <- e.RawJSON:
					default:
					}
				}
			} else if bytes.Contains(msg, []byte("\"response\":true")) {

				var r ResponseMsg
				r.RawJSON = msg

				err := json.Unmarshal(r.RawJSON, &r)
				if err != nil {
					log.Println(err, string(r.RawJSON))
					continue
				}

				if strings.HasPrefix(r.Token, "cmd_dial") {
					if d := atomic.LoadUint32(&b.autoCmd.hangupGap); d > 0 {
						if id := findID([]byte(r.Data)); len(id) > 1 {
							go func() {
								time.Sleep(time.Duration(d) * time.Second)
								b.CmdHangupID(id)
							}()
						}
					}
				}

				if strings.HasPrefix(r.Token, "cmd_auto") {
					r.Ok = true
					b.autoCmd.mux.RLock()
					r.Data = fmt.Sprintf("dial%v;hangupgap=%d",
						b.autoCmd.num,
						atomic.LoadUint32(&b.autoCmd.hangupGap),
					)
					b.autoCmd.mux.RUnlock()
					r.Data = strings.Replace(r.Data, " ", ",", -1)
					r.Data = strings.Replace(r.Data, ":", ";autodialgap=", -1)
					rj, err := json.Marshal(r)
					if err != nil {
						log.Println(err, r.Data)
						continue
					}

					r.RawJSON = rj
				}

				b.responseChan <- r
				if b.wsAddr != "" {
					select {
					case b.responseWsChan <- r.RawJSON:
					default:
					}
				}
			}
		*/
	}
}

func findID(data []byte) string {
	if posA := bytes.Index(data, []byte("call id: ")); posA > 0 {
		if posB := bytes.Index(data[posA:], []byte("\n")); posB > 0 {
			l := len("call id: ")
			return string(data[posA+l : posA+posB])
		}
	}
	return ""
}

// GetEventChan returns the receive-only EventMsg channel for reading data.
func (b *Baresip) GetEventChan() <-chan EventMsg {
	return b.eventChan
}

// GetResponseChan returns the receive-only ResponseMsg channel for reading data.
func (b *Baresip) GetResponseChan() <-chan ResponseMsg {
	return b.responseChan
}

func (b *Baresip) keepActive() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.baresipCtx.Done():
			return

		case <-ticker.C:
			if b.Cmd("uuid", "", "ping") == nil {
				b.successfulPings++
				b.logger.Infof("Ping successful, successful pings: %d", b.successfulPings+1)
			} else {
				// do not terminate the connection on a failed ping...
				b.failedPings++
				b.logger.Infof("Ping failed, failed pings: %d", b.failedPings)
			}
		}
	}
}

// Run a baresip instance
func (b *Baresip) Start() (context.CancelFunc, error) {
	b.baresipCtx, b.baresipCancel = context.WithCancel(context.Background())

	args := []string{"-f", b.configPath, "-p", b.audioPath, "-a", b.userAgent}
	if b.debug {
		args = append(args, "-v")
	}

	// We assume baresip is in the PATH
	b.logger.Infof("Starting baresip with args: %v", args)
	b.baresipCmd = exec.CommandContext(b.baresipCtx, "baresip", args...) //nolint:gosec

	// Open stdout/stderr pipes BEFORE starting the command
	stdout, err := b.baresipCmd.StdoutPipe()
	if err != nil {
		b.baresipCancel()
		return func() {}, fmt.Errorf("error getting baresip stdout pipe: %w", err)
	}
	stderr, err := b.baresipCmd.StderrPipe()
	if err != nil {
		b.baresipCancel()
		return func() {}, fmt.Errorf("error getting baresip stderr pipe: %w", err)
	}

	// Start the baresip command
	if err := b.baresipCmd.Start(); err != nil {
		b.baresipCancel()
		return func() {}, fmt.Errorf("error starting baresip: %w", err)
	}

	// FIXME: these are for debugging purposes, remove in production
	go readOutput("stdout", stdout)
	go readOutput("stderr", stderr)

	// FIXME: implement wait loop with small timeout, to account for baresip startup time
	time.Sleep(500 * time.Millisecond) // Give baresip some time to start
	if err := b.connectCtrl(); err != nil {
		b.baresipCancel()
		return func() {}, err
	}

	// Start reading from the control connection
	go b.read()

	// Simple solution for this https://github.com/baresip/baresip/issues/584
	go b.keepActive()

	return b.baresipCancel, nil
}

func (b *Baresip) connectCtrl() error {
	var err error
	b.ctrlConn, err = net.Dial("tcp", b.ctrlAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to ctrl socket: please make sure ctrl_tcp baresip module is enabled: %w", err)
	}

	// link the TCP socket to the netstring decoder/encoder
	b.ctrlConnDec = netstring.NewDecoder(b.ctrlConn)
	b.ctrlConnEnc = netstring.NewEncoder(b.ctrlConn)
	return nil
}

func (b *Baresip) WaitForShutdown() error {
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

func (b *Baresip) GetStdoutPipe() (io.ReadCloser, error) {
	return b.baresipCmd.StdoutPipe()
}

func (b *Baresip) GetStderrPipe() (io.ReadCloser, error) {
	return b.baresipCmd.StderrPipe()
}

func readOutput(name string, reader io.ReadCloser) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Printf("%s: %s\n", name, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading from %s: %v", name, err)
	}
}

/*
func (b *Baresip) end(err C.int) error {
	if err != 0 {
		C.ua_stop_all(1)
	}

	C.ua_close()
	C.module_app_unload()
	C.conf_close()

	C.baresip_close()

	// Modules must be unloaded after all application activity has stopped.
	C.mod_close()

	C.libre_close()

	// Check for memory leaks
	C.tmr_debug()
	C.mem_debug()

	return fmt.Errorf("%d", err)
}
*/
