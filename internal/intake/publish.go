package intake

// publish.go — S61 T11: the publish trio, ONCE. The engine validates
// against this slice (previewFor's switch keeps its per-mode laws, and its
// default arm now refuses from this list), and `intake.vocabulary` builds
// its descriptor from it — the descriptor and the door cannot drift,
// because there is one set. The LABELS are i18n keys, not English: the
// app owns the words it prints, and the key names where the real words
// live. The STATEMENTS are the daemon's own — they say what the act does,
// in the daemon's voice, and the UI renders them verbatim (P7: an
// authority sentence is never written by the client).

// PublishModes is the closed publish vocabulary: the mode that touches
// nothing, and the two that create a repository outward.
var PublishModes = []string{"none", "private", "public"}

// PublishLabel names the i18n key whose words the app prints for a mode;
// an unknown mode has no key.
func PublishLabel(mode string) string {
	switch mode {
	case "none", "private", "public":
		return "wizard.publish." + mode + ".label"
	}
	return ""
}

// PublishStatement is the confirmation text for a mode — empty for
// `none`, which changes nothing outward and needs no confirmation. The
// public statement says PUBLIC plainly (T11's named test), because a
// soft confirmation of an irreversible act is not a confirmation.
func PublishStatement(mode string) string {
	switch mode {
	case "private":
		return "publish PRIVATE creates a remote repository visible only to the accounts you add — nothing is published to the world, but the content leaves this machine."
	case "public":
		return "publish PUBLIC creates a repository visible to the whole world — every file and its full history become outward publication of your content, readable by anyone, and deleting the repository afterwards does not un-publish what was seen. It needs the confirm-public word, and the daemon refuses without it."
	}
	return ""
}

// IsPublishMode reports membership; previewFor's default arm refuses from
// the list so the sentence and the law name the same trio.
func IsPublishMode(mode string) bool {
	for _, m := range PublishModes {
		if m == mode {
			return true
		}
	}
	return false
}
