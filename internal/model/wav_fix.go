package model

import "encoding/binary"

// repairWAV makes a WAV file's own length fields true. A server that streams speech cannot know the length when it
// writes the header, so it writes 0 or 0xFFFFFFFF (or a stale size) there; Aeon gathers the whole stream before it
// plays, so the real length is known, and a player that trusts the header otherwise stops early, which is the sentence
// "cut off" mid-word. Anything that is not a WAV, or whose lengths are already right, comes back untouched.
func repairWAV(b []byte) []byte {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return b
	}
	out := b
	copied := false
	patch := func(at int, v uint32) {
		if binary.LittleEndian.Uint32(out[at:]) == v {
			return
		}
		if !copied {
			out = append([]byte(nil), b...)
			copied = true
		}
		binary.LittleEndian.PutUint32(out[at:], v)
	}
	patch(4, uint32(len(b)-8))
	for at := 12; at+8 <= len(out); {
		id := string(out[at : at+4])
		size := binary.LittleEndian.Uint32(out[at+4:])
		body := at + 8
		remaining := len(out) - body
		if id == "data" {
			if size == 0 || size == 0xFFFFFFFF || int64(size) > int64(remaining) {
				patch(at+4, uint32(remaining))
			}
			break
		}
		next := int64(body) + int64(size) + int64(size&1) // chunks are word-aligned
		if size == 0xFFFFFFFF || next > int64(len(out)) {
			break
		}
		at = int(next)
	}
	return out
}
