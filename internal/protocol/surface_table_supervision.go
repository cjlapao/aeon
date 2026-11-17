package protocol

// surface_table_supervision.go — S58 T2: the surface table's inbox entries.
//
// Both reads of the attention plane are classified here, beside their own
// family's names (the split-by-cohesion pattern the export/gates files set).
// Reads, because that is what they are: they evaluate live state and write
// nothing. They are NOT HumanDoorOnly flags — the flag marks writes that
// move something real past a person's authority (P34's door); what the
// supervision plane keeps off the narrow surface is a scope ruling (SV-D3,
// the narrow guard stays pinned at nine), and scope.go/narrow tests hold
// that, not a classification here.

func surfaceTableSupervision() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodInboxList: {
			Kind:    SurfaceRead,
			Summary: "Answers what needs a person from live state: items in inbox order with doors, the full source census, nothing stored or cached (SV1).",
		},
		MethodInboxSummary: {
			Kind:    SurfaceRead,
			Summary: "Counts that evaluation by severity, oldest waiting item, source census; acked items count, so the number never lies (SV3).",
		},
		MethodInboxAck: {
			Kind:          SurfaceWrite,
			Summary:       "Marks an item seen: hides it from the default inbox, attributed to the peer, append-only, counted (SV3). Agent peers refused (SV12).",
			HumanDoorOnly: true,
		},
		MethodInboxSnooze: {
			Kind:          SurfaceWrite,
			Summary:       "Hides an item until an instant at most seven days out (SV-D11); expiry computes at read — no job, and a snooze never becomes a deletion.",
			HumanDoorOnly: true,
		},
		MethodInboxUnack: {
			Kind:          SurfaceWrite,
			Summary:       "Brings an acked or snoozed item back to the default inbox; one marker row per changing write, so undoing never duplicates (P3).",
			HumanDoorOnly: true,
		},
		MethodMissionStatus: {
			Kind:    SurfaceRead,
			Summary: "Per-root overview: live loop counts, the inbox.summary twin, spend floor against cap, next-ready, last movement. No score (SV11).",
		},
		MethodNotifyClassList: {
			Kind:    SurfaceRead,
			Summary: "Lists the event-class table: every kind and variant, its tone, templates, enabled state and origin — the truth the resolver reads (SV4).",
		},
		MethodNotifyClassUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Edits one class's enabled flag, tone or templates within the closed placeholders; audited per change, never re-seeded over (SV4, P8).",
			HumanDoorOnly: true,
		},
		MethodNotifyVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The notify plane's descriptor: kinds, severities, placeholders, tones, error and channel kinds, limits — one read, nothing hardcoded.",
		},
		// The channel plane (S58 T5, SV7/SV8): every write moves standing
		// authority to send off the machine, so every write is flagged.
		MethodNotifyChannelCreate: {
			Kind:          SurfaceWrite,
			Summary:       "Adds a webhook channel, born disabled, secret by file-path reference only, host checked at the door (SV7).",
			HumanDoorOnly: true,
		},
		MethodNotifyChannelUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Edits name, url, allowPrivate or the secret — the https rule re-checked, the audit naming fields and hosts, never bodies (SV7, SV12).",
			HumanDoorOnly: true,
		},
		MethodNotifyChannelEnable: {
			Kind:          SurfaceWrite,
			Summary:       "Turns a channel on — refused unless its signing secret resolves; an unsigned webhook is a forgery with a green light (SV7).",
			HumanDoorOnly: true,
		},
		MethodNotifyChannelDisable: {
			Kind:          SurfaceWrite,
			Summary:       "Turns a channel off; events still resolve and the log still records — disabling is a switch, never a deletion (SV12).",
			HumanDoorOnly: true,
		},
		MethodNotifyChannelArchive: {
			Kind:          SurfaceWrite,
			Summary:       "Retires a channel with a reason — never deleted; the delivery log has no FK and outlives it (SV12, archive-only lifecycle).",
			HumanDoorOnly: true,
		},
		MethodNotifyChannelTest: {
			Kind:          SurfaceWrite,
			Summary:       "Sends the test envelope through the real dispatcher and answers with its delivery outcome — a logged delivery, never a dry lie.",
			HumanDoorOnly: true,
		},
		MethodNotifySubscriptionSet: {
			Kind:          SurfaceWrite,
			Summary:       "Upserts one (channel, root) scope row; vocabularies closed, quiet windows all-or-none with an explicit zone (SV8, TR6).",
			HumanDoorOnly: true,
		},
		MethodNotifyChannelList: {
			Kind:    SurfaceRead,
			Summary: "Lists channels as host-plus-bools: enabled, secretPresent, last outcome, consecutive failures — never a url, never a secret.",
		},
		MethodNotifyChannelShow: {
			Kind:    SurfaceRead,
			Summary: "One channel with its subscription rows: the view answers secretPresent and hosts, the same never-a-secret rule as the listing (SV7).",
		},
		MethodNotifyDeliveryList: {
			Kind:    SurfaceRead,
			Summary: "Reads a channel's append-only delivery log — sends, failures, every suppression, honest by construction and body-free by schema (SV12).",
		},
	}
}
