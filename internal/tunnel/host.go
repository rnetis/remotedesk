package tunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"

	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

// HostConfig configures a host agent's connection to the relay.
type HostConfig struct {
	RelayAddr  string        // host:port of the relay
	Signer     ssh.Signer    // this agent's private key
	RelayKey   ssh.PublicKey // relay's public host key to pin (nil = unpinned)
	Unattended bool          // if true, incoming viewers are auto-accepted

	// Handler, if set, serves an authorized viewer stream directly (e.g. the
	// embedded RFB server). If nil, the stream is TCP-forwarded to Target.
	Handler func(io.ReadWriteCloser)
	Target  string // local address to forward viewers to when Handler is nil

	// OnIncoming is called (when not Unattended) to ask for consent before a
	// viewer is bridged. Returning false rejects. If nil, consent is granted.
	OnIncoming func(session string) bool

	Logger *log.Logger
}

// Host is a live registration on the relay.
type Host struct {
	ID  string // connection ID assigned by the relay
	PIN string // one-time session PIN

	cfg   HostConfig
	conn  ssh.Conn
	chans <-chan ssh.NewChannel
	ctl   ssh.Channel
	enc   *json.Encoder
	dec   *json.Decoder
	log   *log.Logger
}

// Connect dials the relay, opens the control channel, and registers, returning
// once an ID + PIN have been assigned. Call Serve to begin handling viewers.
func Connect(cfg HostConfig) (*Host, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	tcp, err := net.Dial("tcp", cfg.RelayAddr)
	if err != nil {
		return nil, fmt.Errorf("dial relay: %w", err)
	}
	clientCfg := &ssh.ClientConfig{
		User:            "host",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Signer)},
		HostKeyCallback: hostKeyCallback(cfg.RelayKey, logger),
	}
	conn, chans, reqs, err := ssh.NewClientConn(tcp, cfg.RelayAddr, clientCfg)
	if err != nil {
		tcp.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	go ssh.DiscardRequests(reqs)

	ctl, ctlReqs, err := conn.OpenChannel(wire.ChanControl, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open control channel: %w", err)
	}
	go ssh.DiscardRequests(ctlReqs)

	h := &Host{
		cfg:   cfg,
		conn:  conn,
		chans: chans,
		ctl:   ctl,
		enc:   json.NewEncoder(ctl),
		dec:   json.NewDecoder(ctl),
		log:   logger,
	}

	if err := h.enc.Encode(wire.ControlMsg{Op: wire.OpRegister, Unattended: cfg.Unattended}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send register: %w", err)
	}
	var msg wire.ControlMsg
	if err := h.dec.Decode(&msg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("await registration: %w", err)
	}
	if msg.Op != wire.OpRegistered {
		conn.Close()
		return nil, fmt.Errorf("unexpected control op %q", msg.Op)
	}
	h.ID, h.PIN = msg.ID, msg.PIN
	return h, nil
}

// Serve handles inbound data channels and consent requests. It blocks until the
// relay connection drops.
func (h *Host) Serve() error {
	go h.acceptData()
	for {
		var msg wire.ControlMsg
		if err := h.dec.Decode(&msg); err != nil {
			return err
		}
		if msg.Op == wire.OpIncoming {
			ok := true
			if !h.cfg.Unattended && h.cfg.OnIncoming != nil {
				ok = h.cfg.OnIncoming(msg.Session)
			}
			reply := wire.OpAccept
			if !ok {
				reply = wire.OpReject
			}
			if err := h.enc.Encode(wire.ControlMsg{Op: reply, Session: msg.Session}); err != nil {
				return err
			}
		}
	}
}

// Close tears down the relay connection.
func (h *Host) Close() error { return h.conn.Close() }

func (h *Host) acceptData() {
	for newCh := range h.chans {
		if newCh.ChannelType() == wire.ChanData {
			go h.handleData(newCh)
		} else {
			newCh.Reject(ssh.UnknownChannelType, "unexpected channel")
		}
	}
}

// handleData bridges one viewer stream to the local target (e.g. the VNC port).
func (h *Host) handleData(newCh ssh.NewChannel) {
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)

	if h.cfg.Handler != nil {
		h.cfg.Handler(ch) // Handler owns closing ch.
		return
	}

	defer ch.Close()
	target, err := net.Dial("tcp", h.cfg.Target)
	if err != nil {
		h.log.Printf("host: cannot reach local target %s: %v", h.cfg.Target, err)
		return
	}
	defer target.Close()
	splice(ch, target)
}
