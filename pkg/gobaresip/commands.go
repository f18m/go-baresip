package gobaresip

import (
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

// CommandMsg struct for ctrl_tcp
type CommandMsg struct {
	Command string `json:"command,omitempty"`
	Params  string `json:"params,omitempty"`
	Token   string `json:"token,omitempty"`
}

// Cmd will send a raw baresip command over ctrl_tcp.
func (b *Baresip) Cmd(command, params, token string) error {
	msg, err := json.Marshal(&CommandMsg{
		Command: command,
		Params:  params,
		Token:   token,
	})
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
func (b *Baresip) CmdAusrc(s string) error {
	c := "ausrc"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
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

// CmdDial will dial number
func (b *Baresip) CmdDial(s string) error {
	c := "dial"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
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
func (b *Baresip) CmdInsmod(s string) error {
	c := "insmod"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
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
func (b *Baresip) CmdRmmod(s string) error {
	c := "rmmod"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
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

// CmdUafind will find User-Agent <aor>
func (b *Baresip) CmdUafind(s string) error {
	c := "uafind"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdUanew will create User-Agent
func (b *Baresip) CmdUanew(s string) error {
	c := "uanew"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdUareg will register <regint> [index]
func (b *Baresip) CmdUareg(s string) error {
	c := "uareg"
	return b.Cmd(c, s, "cmd_"+c+"_"+s)
}

// CmdQuit will quit baresip
func (b *Baresip) CmdQuit() error {
	c := "quit"
	return b.Cmd(c, "", "cmd_"+c)
}
