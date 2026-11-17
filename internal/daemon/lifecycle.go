package daemon

import (
	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/protocol"
)

// # Lifecycle events (T8)
//
// The daemon emits its own. There are no loops, steps or stored events at S2,
// so a subscriber would have nothing to receive — and subscriptions written
// blind now would first be exercised at S5, four slices later, with the loop
// engine as a confounding variable. **A client connecting is a real event**, so
// `aeon watch` in one terminal while another runs `aeon ping` exercises
// filtering, delivery and sequencing for real.

// ClientConnectedPayload is what `client.connected` carries.
//
// The pid comes from the transport's peer credentials (D12, P26) and is 0 where
// the platform does not report one — **a named absence rather than an omitted
// field**, so a reader can tell "no pid available here" from "nobody looked".
type ClientConnectedPayload struct {
	ConnID        string `json:"connId"`
	ClientName    string `json:"clientName,omitempty"`
	ClientVersion string `json:"clientVersion,omitempty"`
	// ProtocolVersion is what was negotiated on this connection.
	ProtocolVersion int `json:"protocolVersion"`
	PeerUID         int `json:"peerUid"`
	// PeerPID is 0 where the platform does not report one (Windows named
	// pipes). The field is always present; the zero is the absence.
	PeerPID int `json:"peerPid"`
	// PeerPIDAvailable distinguishes "pid is 0" from "no pid on this platform",
	// which a bare 0 cannot. Inventing a pid would send somebody to kill the
	// wrong process.
	PeerPIDAvailable bool `json:"peerPidAvailable"`
}

type ClientDisconnectedPayload struct {
	ConnID string `json:"connId"`
}

type SubscriptionCreatedPayload struct {
	SubscriptionID string          `json:"subscriptionId"`
	ConnID         string          `json:"connId"`
	Filter         protocol.Filter `json:"filter"`
	From           uint64          `json:"from"`
}

type DaemonStartedPayload struct {
	DaemonVersion   string `json:"daemonVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
	Endpoint        string `json:"endpoint,omitempty"`
}

// Started emits `daemon.started`. Called once, after Register.
func (d *Daemon) Started(endpoint string) {
	d.publish(protocol.KindDaemonStarted, "", DaemonStartedPayload{
		DaemonVersion:   d.Table.DaemonVersion,
		ProtocolVersion: protocol.PROTOCOL_VERSION,
		Endpoint:        endpoint,
	})
}

// onInitialized emits `client.connected` and logs the peer (D12).
func (d *Daemon) onInitialized(sess *protocol.Session) {
	uid, pid, pidOK := d.peer(sess.ConnID)

	// D12 — the accept log names uid and pid where the platform allows, and
	// says so where it does not. Filesystem permissions are the control (P26);
	// knowing WHICH process connected is what makes the line useful.
	if pidOK {
		d.log.Infof("client connected: conn=%s client=%s/%s protocol=%d uid=%d pid=%d",
			sess.ConnID, sess.ClientName, sess.ClientVersion, sess.Version, uid, pid)
	} else {
		d.log.Infof("client connected: conn=%s client=%s/%s protocol=%d uid=%d pid=unavailable-on-this-platform",
			sess.ConnID, sess.ClientName, sess.ClientVersion, sess.Version, uid)
	}

	d.publish(protocol.KindClientConnected, "", ClientConnectedPayload{
		ConnID:           sess.ConnID,
		ClientName:       sess.ClientName,
		ClientVersion:    sess.ClientVersion,
		ProtocolVersion:  sess.Version,
		PeerUID:          uid,
		PeerPID:          pid,
		PeerPIDAvailable: pidOK,
	})
}

// onSubscribed delivers the cursor's replay, then emits `subscription.created`.
//
// Replay first, then the event: a client that receives `subscription.created`
// before its own backlog would reasonably treat the backlog as live traffic
// arriving after creation, and order its UI by the wrong thing.
func (d *Daemon) onSubscribed(sub *protocol.Subscription, replay []protocol.Event) {
	for _, ev := range replay {
		ev.SubscriptionID = sub.ID
		d.deliver(sub, ev)
	}
	d.publish(protocol.KindSubscriptionCreated, "", SubscriptionCreatedPayload{
		SubscriptionID: sub.ID,
		ConnID:         sub.ConnID,
		Filter:         sub.Filter,
		From:           sub.Next,
	})
}

// onConnectionClosed reaps the connection's subscriptions and announces it.
//
// Triggered by the transport cancelling the connection's context, which it does
// BEFORE closing the socket — so the registry is empty on the way down rather
// than after (Q5, T5). A delivery racing this is not an error: the transport's
// Notify to a closed connection returns nil, which is why nothing here has to
// handle the race.
func (d *Daemon) onConnectionClosed(connID string) {
	// LW9: a terminal outlives a CONNECTION. This frees the dead
	// connection's seats; the shells keep running for whoever attaches next —
	// only `terminal.close`, the reaper, or the daemon's own death ends one.
	if d.terminals != nil {
		d.terminals.DetachConn(connID)
	}
	d.Table.Subs.RemoveConn(connID)
	d.log.Infof("client disconnected: conn=%s", connID)
	d.publish(protocol.KindClientDisconnected, "", ClientDisconnectedPayload{ConnID: connID})
}

// publish allocates a seq and fans the event out to every matching
// subscription.
//
// # Two sequence numbers reach a client, and they are not the same thing
//
// The transport puts its OWN `seq` on the notification envelope: a
// per-connection delivery counter, which advances even when a notification is
// dropped for a slow reader so the client can detect the loss
// (`ConnInfo.NotifyDropped`).
//
// Aeon's `seq` is inside `params`: a stream-scoped event ordinal, and the one
// `subscribe {after: n}` takes.
//
// **A client must use `params.seq` as its cursor, never the envelope's.** The
// envelope's number restarts at reconnection — which is precisely when a cursor
// is needed. The two are complementary rather than competing: a non-zero
// dropped count says a gap exists, and `after` is how the client recovers.
func (d *Daemon) publish(kind, componentRoot string, payload any) {
	d.publishWithNotify(kind, componentRoot, payload, nil)
}

// publishWithNotify is publish, plus an optional delivery-time notify hint.
//
// The hint is attached to `ev` AFTER `Events.Append` returns, never passed
// into it: `Append` is what the durable log and a replayed cursor see, and a
// hint attached here reaches only THIS live delivery (`Event.Notify`'s own
// doc comment says why that is deliberate).
func (d *Daemon) publishWithNotify(kind, componentRoot string, payload any, notify *protocol.NotifyHint) {
	ev, err := d.Table.Events.Append(kind, componentRoot, payload)
	if err != nil {
		d.log.Infof("event %s could not be encoded: %v", kind, err)
		return
	}
	if notify == nil {
		// S58 T4 (SV4/SV5): the class resolver is the ONE source of
		// delivery-time hints — the deleted notifyHintFor switch has no
		// fallback. A storeless daemon (nil resolver) resolves nothing;
		// a hint-less event publishes bare exactly as an uncurated kind
		// always did. An explicit hint from a caller still wins: it is
		// that call site's act, not a re-derivation. S58 T5: the SAME
		// resolution feeds the channel dispatcher at this ONE fan-out
		// point (notify_channels.go); Submit is non-blocking by
		// construction — the loop never waits on a receiver.
		notify = d.resolveForPublish(kind, payload, &ev)
	}
	ev.Notify = notify
	for _, sub := range d.Table.Subs.MatchingHinted(ev.Kind, ev.ComponentRoot, notify != nil) {
		d.deliver(sub, ev)
	}
}

// Publish is publish, exported for later slices' domain events.
func (d *Daemon) Publish(kind, componentRoot string, payload any) {
	d.publish(kind, componentRoot, payload)
}

// PublishNotify is Publish plus an explicit notify hint — the generic door
// any call site (loop lifecycle, a future trigger/automation, anything else
// that reaches a *Daemon) uses to say "this is worth a toast," without
// inventing a new event kind to say it.
func (d *Daemon) PublishNotify(kind, componentRoot string, payload any, notify protocol.NotifyHint) {
	d.publishWithNotify(kind, componentRoot, payload, &notify)
}

func (d *Daemon) deliver(sub *protocol.Subscription, ev protocol.Event) {
	ev.SubscriptionID = sub.ID
	ev.Dseq = d.Table.Subs.NextDelivery(sub.ConnID, sub.ID)
	if err := d.notify(sub.ConnID, "event", ev); err != nil {
		d.log.Infof("event %s could not be delivered to %s: %v", ev.Kind, sub.ConnID, err)
		return
	}
	d.Table.Subs.Advance(sub.ConnID, sub.ID, ev.Seq)
}

// peer reports the connecting process's uid and pid, and whether the pid is
// available at all on this platform.
func (d *Daemon) peer(connID string) (uid, pid int, pidAvailable bool) {
	for _, info := range d.connections() {
		if info.ID == connID {
			return info.PeerUID, info.PeerPID, info.PeerPID != 0
		}
	}
	return 0, 0, false
}

var _ = localrpc.ConnInfo{}
