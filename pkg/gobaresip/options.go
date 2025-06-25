package gobaresip

import "time"

// SetOption takes one or more option function and applies them in order to Baresip.
func (b *Baresip) SetOption(options ...func(*Baresip) error) error {
	for _, opt := range options {
		if err := opt(b); err != nil {
			return err
		}
	}
	return nil
}

// SetCtrlTCPAddr sets the ctrl_tcp modules address.
func SetCtrlTCPAddr(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.ctrlAddr = opt
		return nil
	}
}

// SetLogger sets the logger for Baresip.
func SetLogger(lgr Logger) func(*Baresip) error {
	return func(b *Baresip) error {
		b.logger = lgr
		return nil
	}
}

// SetPingInterval sets the ping interval used as "keep alive" between the baresip server and the
// Baresip Go client. If set to -1, no ping will be sent.
func SetPingInterval(i time.Duration) func(*Baresip) error {
	return func(b *Baresip) error {
		b.pingInterval = i
		return nil
	}
}

// SetCmdWriteTimeout sets the timeout for writing commands on the TCP socket to the baresip server.
// Since commands are typically very short (few bytes), the default timeout is small (100ms).
func SetCmdWriteTimeout(i time.Duration) func(*Baresip) error {
	return func(b *Baresip) error {
		b.ctrlCmdWriteTimeout = i
		return nil
	}
}

// SetCmdResponseTimeout sets the timeout for reading command responses from the TCP socket to the baresip server.
// Since commands are typically very short (few bytes), the default timeout is small (100ms).
func SetCmdResponseTimeout(i time.Duration) func(*Baresip) error {
	return func(b *Baresip) error {
		b.ctrlCmdResponseTimeout = i
		return nil
	}
}

// UseExternalBaresip can be used to disable Baresip from launching a baresip process itself.
// If UseExternalBaresip is used, then [Baresip] expects that an external baresip process is already running
// and that the ctrl_tcp module is enabled and listening on the address set by [SetCtrlTCPAddr].
//
// If [UseExternalBaresip] is used, then the values set by [SetInternalBaresipStartupOptions] and [CaptureInternalBaresipStdoutStderr]
// will be ignored.
func UseExternalBaresip() func(*Baresip) error {
	return func(b *Baresip) error {
		b.runBaresipCmd = false
		return nil
	}
}

// CaptureInternalBaresipStdoutStderr defines whether the Baresip type should capture & log the stdout and stderr
// output of the baresip process, using the logger provided in [SetLogger].
// Don't use this method if you use [UseExternalBaresip], as it will have no effect in that case.
func CaptureInternalBaresipStdoutStderr(logStdout, logStderr bool) func(*Baresip) error {
	return func(b *Baresip) error {
		b.baresipHandle.logStdout = logStdout
		b.baresipHandle.logStderr = logStderr
		return nil
	}
}

// SetInternalBaresipStartupOptions sets the startup options for the baresip process that
// is started by [Baresip].
// Don't use this method if you use [UseExternalBaresip], as it will have no effect in that case.
func SetInternalBaresipStartupOptions(opt BaresipStartOptions) func(*Baresip) error {
	return func(b *Baresip) error {
		b.baresipHandle.startOptions = opt
		return nil
	}
}
