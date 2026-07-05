// Package tunnel provides the SSH client helpers used by the host agent and the
// viewer to talk to the relay. The host registers and serves inbound data
// channels (the ssh -R side); the viewer opens tunneled streams (the ssh -L
// side). All bytes ride inside SSH channels, so each hop is encrypted.
package tunnel

import (
	"io"
	"log"

	"golang.org/x/crypto/ssh"
)

// hostKeyCallback returns a verifier for the relay's host key. If relayKey is
// non-nil the relay is pinned to it; otherwise verification is skipped (only
// acceptable on a trusted/loopback network — callers should pin in production).
func hostKeyCallback(relayKey ssh.PublicKey, logger *log.Logger) ssh.HostKeyCallback {
	if relayKey != nil {
		return ssh.FixedHostKey(relayKey)
	}
	if logger != nil {
		logger.Printf("tunnel: WARNING relay host key not pinned; connection is unauthenticated")
	}
	return ssh.InsecureIgnoreHostKey()
}

// rejectChans rejects every channel the peer tries to open. Used on the viewer
// connection, which never expects inbound channels.
func rejectChans(chans <-chan ssh.NewChannel) {
	for ch := range chans {
		ch.Reject(ssh.Prohibited, "no inbound channels")
	}
}

// splice copies bytes bidirectionally until either side closes, then returns.
func splice(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		io.Copy(dst, src)
		// Unblock the other direction by closing; ignore errors on teardown.
		a.Close()
		b.Close()
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}
