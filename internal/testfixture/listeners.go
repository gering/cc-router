package testfixture

import "net"

// Loopback listens on an ephemeral 127.0.0.1 port.
func Loopback() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }

// AcceptAndClose accepts every connection and closes it at once: a live
// "gateway" for the reachability check, and a transport failure for HTTP.
func AcceptAndClose(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		c.Close()
	}
}

// ClosedPort returns a loopback port nothing listens on (bound, then freed).
func ClosedPort() (int, error) {
	l, err := Loopback()
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}

// Port of a listener.
func Port(l net.Listener) int { return l.Addr().(*net.TCPAddr).Port }
