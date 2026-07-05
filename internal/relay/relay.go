// Package relay implements the rendezvous/broker that runs on a public VPS.
//
// It is an SSH server (golang.org/x/crypto/ssh). Host agents dial out to it and
// register, receiving a connection ID + one-time PIN. Viewers dial in, present
// an ID + PIN, and the relay splices the viewer's byte stream to the matching
// host over SSH channels. The relay never listens on per-session TCP ports:
// both hops are SSH connections initiated outbound by the agents, which is what
// makes this work through NAT without port-forwarding.
package relay

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"remotedesk/internal/id"
	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

// acceptTimeout bounds how long a viewer waits for the host to accept/reject.
const acceptTimeout = 60 * time.Second

// Server is the relay broker.
type Server struct {
	signer ssh.Signer
	log    *log.Logger

	// authorize decides whether an agent's public key may connect. When nil,
	// any key is allowed (fine for a private relay you own; tighten later).
	authorize func(ssh.PublicKey) bool

	mu    sync.Mutex
	hosts map[string]*hostReg // id -> registered host
}

// hostReg is a registered, online host agent.
type hostReg struct {
	id         string
	pin        string
	unattended bool
	conn       *ssh.ServerConn
	fp         string // authorized key fingerprint that owns this ID

	writeMu sync.Mutex // serializes writes to the control channel
	enc     *json.Encoder

	pendMu  sync.Mutex
	pending map[string]chan bool // session -> accept result
}

// New returns a relay Server that presents signer as its SSH host key.
func New(signer ssh.Signer, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		signer: signer,
		log:    logger,
		hosts:  make(map[string]*hostReg),
	}
}

// Serve accepts connections on ln until it errors or is closed.
func (s *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(c)
	}
}

func (s *Server) serverConfig() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if s.authorize != nil && !s.authorize(key) {
				return nil, errors.New("unauthorized key")
			}
			return &ssh.Permissions{
				Extensions: map[string]string{"fp": ssh.FingerprintSHA256(key)},
			}, nil
		},
	}
	cfg.AddHostKey(s.signer)
	return cfg
}

func (s *Server) handleConn(nc net.Conn) {
	defer nc.Close()
	sconn, chans, reqs, err := ssh.NewServerConn(nc, s.serverConfig())
	if err != nil {
		s.log.Printf("relay: handshake from %s failed: %v", nc.RemoteAddr(), err)
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)

	fp := ""
	if sconn.Permissions != nil {
		fp = sconn.Permissions.Extensions["fp"]
	}

	// A connection is classified by the first channel it opens: a host opens a
	// control channel; a viewer opens a connect channel.
	for newCh := range chans {
		switch newCh.ChannelType() {
		case wire.ChanControl:
			s.serveControl(sconn, fp, newCh)
		case wire.ChanConnect:
			go s.serveConnect(newCh)
		default:
			newCh.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

// serveControl handles a host's long-lived control channel. It blocks reading
// control messages until the channel closes, so the owning ServerConn stays up.
func (s *Server) serveControl(sconn *ssh.ServerConn, fp string, newCh ssh.NewChannel) {
	ch, chReqs, err := newCh.Accept()
	if err != nil {
		return
	}
	go ssh.DiscardRequests(chReqs)

	reg := &hostReg{
		conn:    sconn,
		fp:      fp,
		enc:     json.NewEncoder(ch),
		pending: make(map[string]chan bool),
	}

	dec := json.NewDecoder(ch)
	registered := false
	for {
		var msg wire.ControlMsg
		if err := dec.Decode(&msg); err != nil {
			break
		}
		switch msg.Op {
		case wire.OpRegister:
			if registered {
				continue
			}
			reg.unattended = msg.Unattended
			s.register(reg)
			registered = true
			_ = reg.send(wire.ControlMsg{
				Op: wire.OpRegistered, ID: reg.id, PIN: reg.pin,
			})
			s.log.Printf("relay: host %s registered (fp=%s)", reg.id, fp)
		case wire.OpAccept, wire.OpReject:
			reg.resolve(msg.Session, msg.Op == wire.OpAccept)
		}
	}

	if registered {
		s.unregister(reg.id)
		s.log.Printf("relay: host %s disconnected", reg.id)
	}
}

func (s *Server) register(reg *hostReg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Assign an ID that is not currently in use.
	for {
		candidate := id.NewID()
		if _, exists := s.hosts[candidate]; !exists {
			reg.id = candidate
			break
		}
	}
	reg.pin = id.NewPIN()
	s.hosts[reg.id] = reg
}

func (s *Server) unregister(hostID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hosts, hostID)
}

func (s *Server) lookup(hostID string) *hostReg {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hosts[hostID]
}

// serveConnect handles a viewer's connect request end-to-end: validate, get the
// host's consent, then splice the viewer channel to a fresh host data channel.
func (s *Server) serveConnect(newCh ssh.NewChannel) {
	var req wire.ConnectRequest
	if err := json.Unmarshal(newCh.ExtraData(), &req); err != nil {
		newCh.Reject(ssh.Prohibited, "bad connect request")
		return
	}

	reg := s.lookup(req.ID)
	if reg == nil {
		newCh.Reject(ssh.ConnectionFailed, "no such host online")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.PIN), []byte(reg.pin)) != 1 {
		newCh.Reject(ssh.Prohibited, "invalid PIN")
		return
	}

	session := id.NewID()
	if !s.awaitConsent(reg, session) {
		newCh.Reject(ssh.Prohibited, "rejected by host")
		return
	}

	viewerCh, viewerReqs, err := newCh.Accept()
	if err != nil {
		return
	}
	defer viewerCh.Close()
	go ssh.DiscardRequests(viewerReqs)

	// Open the matching data channel to the host and splice the two together.
	payload, _ := json.Marshal(wire.DataOpen{Session: session})
	dataCh, dataReqs, err := reg.conn.OpenChannel(wire.ChanData, payload)
	if err != nil {
		s.log.Printf("relay: host %s refused data channel: %v", reg.id, err)
		return
	}
	defer dataCh.Close()
	go ssh.DiscardRequests(dataReqs)

	s.log.Printf("relay: bridging viewer -> host %s (session %s)", reg.id, session)
	splice(viewerCh, dataCh)
}

// awaitConsent notifies the host of an incoming viewer and waits for a reply.
// Unattended hosts consent automatically.
func (s *Server) awaitConsent(reg *hostReg, session string) bool {
	if reg.unattended {
		return true
	}
	result := make(chan bool, 1)
	reg.pendMu.Lock()
	reg.pending[session] = result
	reg.pendMu.Unlock()
	defer func() {
		reg.pendMu.Lock()
		delete(reg.pending, session)
		reg.pendMu.Unlock()
	}()

	if err := reg.send(wire.ControlMsg{Op: wire.OpIncoming, Session: session}); err != nil {
		return false
	}
	select {
	case ok := <-result:
		return ok
	case <-time.After(acceptTimeout):
		return false
	}
}

func (r *hostReg) send(msg wire.ControlMsg) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.enc.Encode(msg)
}

func (r *hostReg) resolve(session string, ok bool) {
	r.pendMu.Lock()
	ch := r.pending[session]
	r.pendMu.Unlock()
	if ch != nil {
		select {
		case ch <- ok:
		default:
		}
	}
}

// splice copies bytes bidirectionally between a and b until either side closes.
func splice(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}

// FingerprintSHA256 is re-exported for callers that build authorize funcs.
func FingerprintSHA256(key ssh.PublicKey) string { return ssh.FingerprintSHA256(key) }
