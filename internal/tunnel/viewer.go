package tunnel

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

// ViewerConfig configures a viewer's connection to the relay.
type ViewerConfig struct {
	RelayAddr string
	Signer    ssh.Signer
	RelayKey  ssh.PublicKey
	ID        string // target host connection ID
	PIN       string // session PIN
	Logger    *log.Logger
}

// Stream is a tunneled byte stream to the host's local target. It is an
// io.ReadWriteCloser; closing it also closes the underlying SSH connection.
type Stream struct {
	ssh.Channel
	conn ssh.Conn
}

// Close closes the channel and the SSH connection behind it.
func (s *Stream) Close() error {
	err := s.Channel.Close()
	s.conn.Close()
	return err
}

func dialViewer(cfg ViewerConfig) (ssh.Conn, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	tcp, err := net.Dial("tcp", cfg.RelayAddr)
	if err != nil {
		return nil, fmt.Errorf("dial relay: %w", err)
	}
	clientCfg := &ssh.ClientConfig{
		User:            "viewer",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Signer)},
		HostKeyCallback: hostKeyCallback(cfg.RelayKey, logger),
	}
	conn, chans, reqs, err := ssh.NewClientConn(tcp, cfg.RelayAddr, clientCfg)
	if err != nil {
		tcp.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	go ssh.DiscardRequests(reqs)
	go rejectChans(chans)
	return conn, nil
}

// Dial opens one tunneled stream to the host identified by cfg.ID. The relay
// asks the host to consent before the channel opens; a rejection surfaces as an
// error here.
func Dial(cfg ViewerConfig) (*Stream, error) {
	conn, err := dialViewer(cfg)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(wire.ConnectRequest{ID: cfg.ID, PIN: cfg.PIN})
	ch, chReqs, err := conn.OpenChannel(wire.ChanConnect, payload)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("connect to host: %w", err)
	}
	go ssh.DiscardRequests(chReqs)
	return &Stream{Channel: ch, conn: conn}, nil
}

// LocalForward listens on listenAddr and, for each accepted local connection,
// opens a fresh tunnel to the host and splices the two — the ssh -L equivalent.
// This lets any standard VNC viewer connect to listenAddr. It blocks until the
// listener errors.
func LocalForward(cfg ViewerConfig, listenAddr string) error {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Printf("viewer: forwarding %s -> host %s via relay", listenAddr, cfg.ID)
	for {
		local, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer local.Close()
			stream, err := Dial(cfg)
			if err != nil {
				logger.Printf("viewer: tunnel failed: %v", err)
				return
			}
			defer stream.Close()
			splice(local, stream)
		}()
	}
}
