//go:build linux

package endpoint

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerPID reads the holder's pid from the connected socket.
//
// SO_PEERCRED is the same affordance localrpc uses for peer credentials, and
// it is why a refusal here can name the process rather than just the path.
func peerPID(conn net.Conn) (int, bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}
	var cred *unix.Ucred
	var credErr error
	if ctlErr := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); ctlErr != nil || credErr != nil || cred == nil {
		return 0, false
	}
	return int(cred.Pid), true
}
