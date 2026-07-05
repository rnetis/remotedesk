// Package wire defines the small control protocol that rides inside SSH
// channels between the relay, the host agent, and the viewer.
//
// Everything is tunneled as named SSH channels, so the transport is encrypted
// end-to-end on each hop (host<->relay and viewer<->relay). Channel types:
//
//   - ChanControl : opened by the host after it connects. Long-lived. Carries
//     newline-delimited JSON control messages (register, incoming, accept).
//   - ChanConnect : opened by the viewer. Its ExtraData carries a JSON
//     ConnectRequest (id + pin). Once authorized this channel becomes the raw
//     tunneled byte stream to the host's local VNC port.
//   - ChanData    : opened by the relay *to the host* when a viewer is
//     authorized. The host dials its local target and pipes bytes; the relay
//     splices this together with the viewer's ChanConnect channel.
package wire

import (
	"crypto/ed25519"
	"crypto/rand"

	"golang.org/x/crypto/ssh"
)

// SSH channel type names used by remotedesk.
const (
	ChanControl = "rd-control@remotedesk"
	ChanConnect = "rd-connect@remotedesk"
	ChanData    = "rd-data@remotedesk"
)

// Control message operations (the "op" field of ControlMsg).
const (
	OpRegister   = "register"   // host -> relay: request an ID + PIN
	OpRegistered = "registered" // relay -> host: assigned ID + PIN
	OpIncoming   = "incoming"   // relay -> host: a viewer wants to connect
	OpAccept     = "accept"     // host -> relay: allow the pending viewer
	OpReject     = "reject"     // host -> relay: deny the pending viewer
	OpError      = "error"      // relay -> host: something went wrong
)

// ControlMsg is a single newline-delimited JSON message on the control channel.
type ControlMsg struct {
	Op string `json:"op"`

	// OpRegister
	Unattended bool `json:"unattended,omitempty"`

	// OpRegistered
	ID  string `json:"id,omitempty"`
	PIN string `json:"pin,omitempty"`

	// OpIncoming / OpAccept / OpReject: correlates a viewer request.
	Session string `json:"session,omitempty"`
	From    string `json:"from,omitempty"`

	// OpError
	Error string `json:"error,omitempty"`
}

// ConnectRequest is marshaled into the ExtraData of a viewer's ChanConnect.
type ConnectRequest struct {
	ID  string `json:"id"`
	PIN string `json:"pin"`
}

// DataOpen is marshaled into the ExtraData of a relay->host ChanData channel so
// the host knows which viewer session the byte stream belongs to.
type DataOpen struct {
	Session string `json:"session"`
}

// NewSigner generates a fresh ed25519 SSH signer. Used to mint host/agent keys
// on first run when none exists on disk.
func NewSigner() (ssh.Signer, ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return signer, priv, nil
}
