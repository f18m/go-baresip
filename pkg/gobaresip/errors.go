package gobaresip

import "errors"

// ErrNoCtrlConn is returned when trying to send a command to [Baresip] but the control connection has never
// been estabilished. Did you invoke the [Baresip.Serve] method?
var ErrNoCtrlConn = errors.New("no control connection established")
