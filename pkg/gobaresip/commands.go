package gobaresip

import (
	"context"
	"strconv"
	"time"

	"github.com/goccy/go-json"
	"github.com/markdingo/netstring"
)

/*
	See https://github.com/baresip/baresip/wiki/Commands-registry

  /100rel ..                   Set 100rel mode
  /about                       About box
  /accept             a        Accept incoming call
  /acceptdir ..                Accept incoming call with audio and videodirection.
  /addcontact ..               Add a contact
  /answermode ..               Set answer mode
  /apistate                    User Agent state
  /aufileinfo ..               Audio file info
  /auplay ..                   Switch audio player
  /ausrc ..                    Switch audio source
  /callstat           c        Call status
  /conf_reload                 Reload config file
  /config                      Print configuration
  /contact_next       >        Set next contact
  /contact_prev       <        Set previous contact
  /contacts           C        List contacts
  /dial ..            d ..     Dial
  /dialcontact        D        Dial current contact
  /dialdir ..                  Dial with audio and videodirection.
  /dnd ..                      Set Do not Disturb
  /entransp ..                 Enable/Disable transport
  /hangup             b        Hangup call
  /hangupall ..                Hangup all calls with direction
  /help               h        Help menu
  /insmod ..                   Load module
  /listcalls          l        List active calls
  /loglevel           v        Log level toggle
  /main                        Main loop debug
  /memstat            y        Memory status
  /message ..         M ..     Message current contact
  /modules                     Module debug
  /netchange                   Inform netroam about a network change
  /netstat            n        Network debug
  /options ..         o ..     Options
  /play ..                     Play audio file
  /quit               q        Quit
  /refer ..           R ..     Send REFER outside dialog
  /reginfo            r        Registration info
  /rmcontact ..                Remove a contact
  /rmmod ..                    Unload module
  /setadelay ..                Set answer delay for outgoing call
  /setansval ..                Set value for Call-Info/Alert-Info
  /sipstat            i        SIP debug
  /sysinfo            s        System info
  /timers                      Timer debug
  /tlsissuer                   TLS certificate issuer
  /tlssubject                  TLS certificate subject
  /uaaddheader ..              Add custom header to UA
  /uadel ..                    Delete User-Agent
  /uadelall ..                 Delete all User-Agents
  /uafind ..                   Find User-Agent <aor>
  /uanew ..                    Create User-Agent
  /uareg ..                    UA register <regint> [index]
  /uarmheader ..               Remove custom header from UA
  /uastat             u        UA debug
  /uuid                        Print UUID
  /vidsrc ..                   Switch video source

*/

// CommandMsg defines the JSON structure accepted by Baresip ctrl_tcp socket
// See doxygen docs at https://github.com/baresip/baresip/blob/main/modules/ctrl_tcp/ctrl_tcp.c
type CommandMsg struct {
	Command string `json:"command,omitempty"`
	Params  string `json:"params,omitempty"`
	Token   string `json:"token,omitempty"`
}

// Cmd will send a raw baresip command over the control TCP socket but will not wait
// for any acknowledge. It might return an error in case the write on TCP socket is erroring out though.
func (b *Baresip) Cmd(command, params, token string) error {
	return b.CmdTx(CommandMsg{
		Command: command,
		Params:  params,
		Token:   token,
	})
}

// CmdTx will send a raw baresip command over the control TCP socket but will not wait
// for any acknowledge. It might return an error in case the write on TCP socket is erroring out though.
func (b *Baresip) CmdTx(cmd CommandMsg) error {
	if b.ctrlConn == nil {
		return ErrNoCtrlConn
	}

	msg, err := json.Marshal(&cmd)
	if err != nil {
		return err
	}

	success := false
	b.ctrlConnTxMutex.Lock()
	err = b.ctrlConn.SetWriteDeadline(time.Now().Add(b.ctrlCmdWriteTimeout))
	if err == nil {
		err = b.ctrlConnEnc.EncodeString(netstring.NoKey, string(msg))
		if err == nil {
			success = true
		}
	}
	b.ctrlConnTxMutex.Unlock()

	if success {
		b.ctrlStats.TxStats.SuccessfulCmds++
	} else {
		b.ctrlStats.TxStats.FailedCmds++
	}

	return err
}

// CmdTxWithAck will send a raw baresip command over the control TCP socket and will wait
// for the command response, i.e. baresip acknowledgement, and will return it.
// When using this function, do not simultaneously try to read from the channel returned by
// [GetResponseChan].
// This function expects a single response to be read in the
// TCP socket buffer and will discard and response whose token does not match with [cmd.Token].
// Note that proper error checking should look like:
//
//	if resp, err := baresipInstance.CmdTxWithAck(...); err != nil || !resp.Ok {
//	    // ... handle the error: err != nil indicates a TX failure; resp.Ok == false indicates a command failed inside Baresip
//	}
func (b *Baresip) CmdTxWithAck(cmd CommandMsg) (ResponseMsg, error) {
	if err := b.CmdTx(cmd); err != nil {
		return ResponseMsg{}, err
	}

	ctx, cancelFn := context.WithTimeout(context.Background(), b.ctrlCmdResponseTimeout)
	defer cancelFn()

	// Block till either the context is cancelled or we get the response back
	for {
		select {
		case <-ctx.Done():
			b.logger.Infof("command response read timed out: %s", ctx.Err())
			return ResponseMsg{}, ctx.Err()

		case response := <-b.responseChan:
			if response.Token != cmd.Token {
				b.logger.Infof("received a response for another (previously sent) command: %+v... discarding", response)
				// keep looping and keep waiting for another response
			} else {
				// completed
				return response, nil
			}
		}
	}
}

// CmdAccept will accept incoming call
func (b *Baresip) CmdAccept() error {
	c := "accept"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdAcceptdir will accept incoming call with audio and videodirection.
func (b *Baresip) CmdAcceptdir(s string) error {
	c := "acceptdir"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAnswermode will set answer mode
func (b *Baresip) CmdAnswermode(s string) error {
	c := "answermode"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAuplay will switch audio player
func (b *Baresip) CmdAuplay(s string) error {
	c := "auplay"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAusrc will switch audio source
func (b *Baresip) CmdAusrc(driver, device string) error {
	c := "ausrc"
	return b.Cmd(c, driver+","+device, "cmd_"+c+"_"+driver+"_"+device)
}

// CmdCallstat will show call status
func (b *Baresip) CmdCallstat() error {
	c := "callstat"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdContactNext will set next contact
func (b *Baresip) CmdContactNext() error {
	c := "contact_next"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdContactPrev will set previous contact
func (b *Baresip) CmdContactPrev() error {
	c := "contact_prev"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdAutodial will dial number automatically
func (b *Baresip) CmdAutodial(s string) error {
	c := "autodial dial"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAutodialdelay will set delay before auto dial [ms]
func (b *Baresip) CmdAutodialdelay(n int) error {
	c := "autodialdelay"
	return b.Cmd(c, strconv.Itoa(n), "cmd_"+c+"_"+strconv.Itoa(n))
}

// CmdDial will start an outgoing call to the provided SIP URI.
func (b *Baresip) CmdDial(calledsipURI string) error {
	c := "dial"
	return b.Cmd(c, calledsipURI, "cmd_"+c+"_"+calledsipURI)
}

// CmdDialcontact will dial current contact
func (b *Baresip) CmdDialcontact() error {
	c := "dialcontact"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdDialdir will dial with audio and videodirection
func (b *Baresip) CmdDialdir(s string) error {
	c := "dialdir"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdAutohangup will hangup call automatically
func (b *Baresip) CmdAutohangup() error {
	c := "autohangup"
	return b.Cmd(c, "hangup", "cmd_"+c)
}

// CmdAutohangupdelay will set delay before hangup [ms]
func (b *Baresip) CmdAutohangupdelay(n int) error {
	c := "autohangupdelay"
	return b.Cmd(c, strconv.Itoa(n), "cmd_"+c+"_"+strconv.Itoa(n))
}

// CmdHangup will hangup call
func (b *Baresip) CmdHangup() error {
	c := "hangup"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdHangupID will hangup call with Call-ID
func (b *Baresip) CmdHangupID(callID string) error {
	c := "hangup"
	return b.Cmd(c, callID, "cmd_"+c)
}

// CmdHangupall will hangup all calls with direction
func (b *Baresip) CmdHangupall(s string) error {
	c := "hangupall"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdInsmod will load module
func (b *Baresip) CmdInsmod(moduleName string) error {
	c := "insmod"
	return b.Cmd(c, moduleName, "cmd_"+c+"_"+moduleName)
}

// CmdListcalls will list active calls
func (b *Baresip) CmdListcalls() error {
	c := "listcalls"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdReginfo will list registration info
func (b *Baresip) CmdReginfo() error {
	c := "reginfo"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdRmmod will unload module
func (b *Baresip) CmdRmmod(moduleName string) error {
	c := "rmmod"
	return b.Cmd(c, moduleName, "cmd_"+c+"_"+moduleName)
}

// CmdSetadelay will set answer delay for outgoing call
func (b *Baresip) CmdSetadelay(n int) error {
	c := "setadelay"
	return b.Cmd(c, strconv.Itoa(n), "cmd_"+c+"_"+strconv.Itoa(n))
}

// CmdUadel will delete User-Agent
func (b *Baresip) CmdUadel(s string) error {
	c := "uadel"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdUadelall will delete all User-Agents
func (b *Baresip) CmdUadelall() error {
	c := "uadelall"
	return b.Cmd(c, "", "cmd_"+c)
}

// CmdUafind will find User-Agent <sipURI>
func (b *Baresip) CmdUafind(sipURI string) error {
	c := "uafind"
	return b.Cmd(c, sipURI, "cmd_"+c+"_"+sipURI)
}

// CmdUanew will create User-Agent
func (b *Baresip) CmdUanew(sipURI string) error {
	c := "uanew"
	return b.Cmd(c, sipURI, "cmd_"+c+"_"+sipURI)
}

// CmdUareg will register <regint> [index]
func (b *Baresip) CmdUareg(sipURI string) error {
	c := "uareg"
	return b.Cmd(c, sipURI, "cmd_"+c+"_"+sipURI)
}

// CmdQuit will quit baresip
func (b *Baresip) CmdQuit() error {
	c := "quit"
	return b.Cmd(c, "", "cmd_"+c)
}
