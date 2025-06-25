package gobaresip

import (
	"context"
	"fmt"
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

// CommandMsg defines the JSON structure accepted by Baresip ctrl_tcp socket.
// See doxygen docs at https://github.com/baresip/baresip/blob/main/modules/ctrl_tcp/ctrl_tcp.c
type CommandMsg struct {
	Command string `json:"command,omitempty"`
	Params  string `json:"params,omitempty"`
	Token   string `json:"token,omitempty"`
}

// Cmd will send a raw baresip command over the control TCP socket but will not wait
// for any acknowledge. It might return an error in case the write on TCP socket is erroring out though.
// When using [Cmd] you must have a goroutine reading the response from the channel returned by [Baresip.GetResponseChan].
// A nil error returned by this function indicates only the TX of the command was successful;
// only the [ResponseMsg] from the response channel indicates if the command was executed successfully by baresip.
func (b *Baresip) Cmd(command, params, token string) error {
	return b.CmdTx(CommandMsg{
		Command: command,
		Params:  params,
		Token:   token,
	})
}

// CmdTx will send a raw baresip command over the control TCP socket but will not wait
// for any acknowledge. It might return an error in case the write on TCP socket is erroring out though.
// When using [CmdTx] you must have a goroutine reading the response from the channel returned by [Baresip.GetResponseChan].
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
func (b *Baresip) CmdTxWithAck(cmd CommandMsg) (ResponseMsg, error) {
	if cmd.Token == "" {
		// we later want to check that the response token is matching, so make sure the token is non-empty:
		cmd.Token = cmd.Command + "_token"
	}

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
				// completed.. successfully or not?
				if !response.Ok {
					return response, fmt.Errorf("%w: %s", ErrFailedCmd, response.Data)
				}

				// success!
				return response, nil
			}
		}
	}
}

// CmdAccept will accept incoming call
func (b *Baresip) CmdAccept() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "accept", Params: ""})
}

// CmdAcceptdir will accept incoming call with audio and videodirection.
func (b *Baresip) CmdAcceptdir(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "acceptdir", Params: s})
}

// CmdAnswermode will set answer mode
func (b *Baresip) CmdAnswermode(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "answermode", Params: s})
}

// CmdAuplay will switch audio player
func (b *Baresip) CmdAuplay(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "auplay", Params: s})
}

// CmdAusrc will switch audio source
func (b *Baresip) CmdAusrc(driver, device string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "ausrc", Params: driver + "," + device})
}

// CmdCallstat will show call status
func (b *Baresip) CmdCallstat() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "callstat", Params: ""})
}

// CmdContactNext will set next contact
func (b *Baresip) CmdContactNext() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "contact_next", Params: ""})
}

// CmdContactPrev will set previous contact
func (b *Baresip) CmdContactPrev() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "contact_prev", Params: ""})
}

// CmdAutodial will dial number automatically
func (b *Baresip) CmdAutodial(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "autodial dial", Params: s + "_" + s})
}

// CmdAutodialdelay will set delay before auto dial [ms]
func (b *Baresip) CmdAutodialdelay(n int) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "autodialdelay", Params: strconv.Itoa(n) + "_" + strconv.Itoa(n)})
}

// CmdDial will start an outgoing call to the provided SIP URI.
func (b *Baresip) CmdDial(calledsipURI string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "dial", Params: calledsipURI + "_" + calledsipURI})
}

// CmdDialcontact will dial current contact
func (b *Baresip) CmdDialcontact() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "dialcontact", Params: ""})
}

// CmdDialdir will dial with audio and videodirection
func (b *Baresip) CmdDialdir(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "dialdir", Params: s + "_" + s})
}

// CmdAutohangup will hangup call automatically
func (b *Baresip) CmdAutohangup() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "autohangup", Params: "hangup"})
}

// CmdAutohangupdelay will set delay before hangup [ms]
func (b *Baresip) CmdAutohangupdelay(n int) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "autohangupdelay", Params: strconv.Itoa(n) + "_" + strconv.Itoa(n)})
}

// CmdHangup will hangup call
func (b *Baresip) CmdHangup() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "hangup", Params: ""})
}

// CmdHangupID will hangup call with Call-ID
func (b *Baresip) CmdHangupID(callID string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "hangup", Params: callID})
}

// CmdHangupall will hangup all calls with direction
func (b *Baresip) CmdHangupall(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "hangupall", Params: s + "_" + s})
}

// CmdInsmod will load module
func (b *Baresip) CmdInsmod(moduleName string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "insmod", Params: moduleName + "_" + moduleName})
}

// CmdListcalls will list active calls
func (b *Baresip) CmdListcalls() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "listcalls", Params: ""})
}

// CmdReginfo will list registration info
func (b *Baresip) CmdReginfo() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "reginfo", Params: ""})
}

// CmdRmmod will unload module
func (b *Baresip) CmdRmmod(moduleName string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "rmmod", Params: moduleName + "_" + moduleName})
}

// CmdSetadelay will set answer delay for outgoing call
func (b *Baresip) CmdSetadelay(n int) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "setadelay", Params: strconv.Itoa(n) + "_" + strconv.Itoa(n)})
}

// CmdUadel will delete User-Agent
func (b *Baresip) CmdUadel(s string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "uadel", Params: s + "_" + s})
}

// CmdUadelall will delete all User-Agents
func (b *Baresip) CmdUadelall() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "uadelall", Params: ""})
}

// CmdUafind will find User-Agent <sipURI>
func (b *Baresip) CmdUafind(sipURI string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "uafind", Params: sipURI + "_" + sipURI})
}

// CmdUanew will create User-Agent
func (b *Baresip) CmdUanew(sipURI string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "uanew", Params: sipURI + "_" + sipURI})
}

// CmdUareg will register <regint> [index]
func (b *Baresip) CmdUareg(sipURI string) (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "uareg", Params: sipURI + "_" + sipURI})
}

// CmdQuit will quit baresip
func (b *Baresip) CmdQuit() (ResponseMsg, error) {
	return b.CmdTxWithAck(CommandMsg{Command: "quit", Params: ""})
}
