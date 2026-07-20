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

// Defaults for the hardening knobs in Options.
const (
	defaultMaxPINAttempts   = 5
	defaultLockout          = 30 * time.Second
	defaultHandshakeTimeout = 15 * time.Second
	defaultMaxConns         = 512
)

// Options configures a relay Server. The zero value of each field falls back to
// a sensible default, so callers can set only what they care about.
type Options struct {
	// Signer is the relay's SSH host key. Required.
	Signer ssh.Signer
	// Logger receives operational logs. Defaults to log.Default().
	Logger *log.Logger
	// Authorize decides whether an agent's public key may connect. When nil,
	// any key is allowed (fine for a private relay you own). Set it to an
	// allowlist to restrict which agents can reach the relay.
	Authorize func(ssh.PublicKey) bool
	// MaxPINAttempts is how many wrong PINs a host registration tolerates before
	// further connection attempts to it are locked out for LockoutDuration.
	MaxPINAttempts int
	// LockoutDuration is how long a host is protected after too many bad PINs.
	LockoutDuration time.Duration
	// HandshakeTimeout bounds the SSH handshake and the wait for a peer's first
	// channel, so a slow client cannot tie up a connection indefinitely.
	HandshakeTimeout time.Duration
	// MaxConns caps concurrent connections the relay will service at once.
	MaxConns int
}

// Server is the relay broker.
type Server struct {
	signer ssh.Signer
	log    *log.Logger

	// authorize decides whether an agent's public key may connect. When nil,
	// any key is allowed (fine for a private relay you own; tighten later).
	authorize func(ssh.PublicKey) bool

	maxPINAttempts   int
	lockout          time.Duration
	handshakeTimeout time.Duration
	sem              chan struct{} // bounds concurrent connections

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

	// Brute-force protection for the connection PIN.
	attemptMu sync.Mutex
	failures  int
	lockUntil time.Time
}

// New returns a relay Server that presents signer as its SSH host key, using
// default hardening options.
func New(signer ssh.Signer, logger *log.Logger) *Server {
	return NewWithOptions(Options{Signer: signer, Logger: logger})
}

// NewWithOptions returns a relay Server configured by opts.
func NewWithOptions(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	if opts.MaxPINAttempts <= 0 {
		opts.MaxPINAttempts = defaultMaxPINAttempts
	}
	if opts.LockoutDuration <= 0 {
		opts.LockoutDuration = defaultLockout
	}
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = defaultHandshakeTimeout
	}
	if opts.MaxConns <= 0 {
		opts.MaxConns = defaultMaxConns
	}
	return &Server{
		signer:           opts.Signer,
		log:              opts.Logger,
		authorize:        opts.Authorize,
		maxPINAttempts:   opts.MaxPINAttempts,
		lockout:          opts.LockoutDuration,
		handshakeTimeout: opts.HandshakeTimeout,
		sem:              make(chan struct{}, opts.MaxConns),
		hosts:            make(map[string]*hostReg),
	}
}

// Serve accepts connections on ln until the listener is closed. Closing ln (see
// graceful shutdown in cmd/relayd) makes Serve return nil. Transient accept
// errors are retried with capped backoff rather than bringing the relay down.
func (s *Server) Serve(ln net.Listener) error {
	var tempDelay time.Duration
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			// Back off on transient errors (e.g. fd exhaustion) instead of
			// spinning or dying.
			if tempDelay == 0 {
				tempDelay = 5 * time.Millisecond
			} else {
				tempDelay *= 2
			}
			if tempDelay > time.Second {
				tempDelay = time.Second
			}
			s.log.Printf("relay: accept error: %v; retrying in %s", err, tempDelay)
			time.Sleep(tempDelay)
			continue
		}
		tempDelay = 0

		// Cap concurrency: drop new connections once saturated so a flood cannot
		// exhaust memory/goroutines.
		select {
		case s.sem <- struct{}{}:
			go func() {
				defer func() { <-s.sem }()
				s.handleConn(c)
			}()
		default:
			s.log.Printf("relay: connection limit reached, dropping %s", c.RemoteAddr())
			c.Close()
		}
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

	// Bound the handshake and the wait for the peer's first channel so a
	// slow-loris client cannot hold a connection open doing nothing.
	nc.SetDeadline(time.Now().Add(s.handshakeTimeout))
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
	// control channel; a viewer opens a connect channel. Once a legitimate peer
	// has opened its first channel, clear the deadline: host control channels are
	// long-lived and idle by design, and viewer data flows on their own timing.
	first := true
	for newCh := range chans {
		if first {
			nc.SetDeadline(time.Time{})
			first = false
		}
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

// authResult is the outcome of checking a viewer's PIN against a host.
type authResult int

const (
	authOK     authResult = iota // PIN correct
	authBadPIN                   // PIN wrong, host not (yet) locked
	authLocked                   // host is locked out; attempt refused without checking
)

// authenticate validates pin against reg's current PIN with brute-force
// protection. Wrong PINs accrue toward a lockout; while locked, every attempt
// (right or wrong) is refused. justLocked is true only on the attempt that trips
// the lockout, so the caller can notify the host exactly once per lockout.
func (s *Server) authenticate(reg *hostReg, pin string) (res authResult, justLocked bool) {
	reg.attemptMu.Lock()
	defer reg.attemptMu.Unlock()

	if time.Now().Before(reg.lockUntil) {
		return authLocked, false
	}
	if subtle.ConstantTimeCompare([]byte(pin), []byte(reg.pin)) == 1 {
		reg.failures = 0
		return authOK, false
	}
	reg.failures++
	if reg.failures >= s.maxPINAttempts {
		reg.failures = 0
		reg.lockUntil = time.Now().Add(s.lockout)
		return authLocked, true
	}
	return authBadPIN, false
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
	switch res, justLocked := s.authenticate(reg, req.PIN); res {
	case authOK:
		// proceed
	case authLocked:
		if justLocked {
			s.log.Printf("relay: host %s locked out after %d bad PINs", reg.id, s.maxPINAttempts)
			_ = reg.send(wire.ControlMsg{
				Op:    wire.OpAlert,
				Error: "multiple failed connection attempts detected; new attempts are temporarily blocked",
			})
		}
		newCh.Reject(ssh.Prohibited, "too many attempts; try again later")
		return
	default: // authBadPIN
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
