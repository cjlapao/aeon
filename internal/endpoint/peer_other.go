//go:build !linux

package endpoint

import "net"

// peerPID is unavailable on this platform.
//
// Saying so is better than inventing a number: a refusal naming the wrong pid
// sends somebody to kill the wrong process.
func peerPID(net.Conn) (int, bool) { return 0, false }
