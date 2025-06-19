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

// SetConfigPath sets the config path.
func SetConfigPath(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.configPath = opt
		return nil
	}
}

// SetAudioPath sets the audio path.
func SetAudioPath(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.audioPath = opt
		return nil
	}
}

// SetBaresipDebug sets the debug mode on the baresip server (-v command-line option).
func SetBaresipDebug(opt bool) func(*Baresip) error {
	return func(b *Baresip) error {
		b.debug = opt
		return nil
	}
}

// SetUserAgent sets the UserAgent.
func SetUserAgent(opt string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.userAgent = opt
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

// SetLogBaresipStdoutAndStderr defines whether the Baresip type should capture & log the stdout and stderr
// output of the baresip process, using the logger provided in [SetLogger].
func SetLogBaresipStdoutAndStderr(logStdout, logStderr bool) func(*Baresip) error {
	return func(b *Baresip) error {
		b.logStdout = logStdout
		b.logStderr = logStderr
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
