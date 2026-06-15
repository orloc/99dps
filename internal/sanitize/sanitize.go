// Package sanitize strips player/NPC communication out of EQ log lines, so logs
// (or derived data) can be shared without leaking private chat. It targets every
// conversation channel — tell, guild, group, raid, OOC, auction, shout, say —
// identified by the quoted-speech marker each one logs.
//
// It is deliberately aggressive about the "say" channel: NPC dialog and a pet's
// "My leader is X." line are on that channel too, so they're stripped as well.
// That's fine — sanitize runs only on OUTBOUND data (a scrubbed log, or events
// about to be shared with other clients). The local parser always sees the full,
// untouched log, so pet attribution and the rest are unaffected.
package sanitize

import (
	"bufio"
	"io"
	"strings"
)

// chatMarkers are substrings that mark a communication line. Combat/mechanics
// lines carry no quoted speech, so these never collide with them. Each channel
// is covered in both its third-person and first-person ("You …") form.
var chatMarkers = []string{
	" tells you, '",                                   // incoming tell
	"You told ",                                       // outgoing tell ("You told X '…'", no comma)
	" tells the guild, '", "You say to your guild, '", // guild
	" tells the group, '", "You tell your party, '", // group
	" tells the raid, '", "You tell the raid, '", // raid
	"out of character, '",            // OOC (both directions)
	" auctions, '", "You auction, '", // auction
	" shouts, '", "You shout, '", // shout
	" says, '", "You say, '", // say (incl. NPC / pet dialog)
}

// IsChat reports whether a log line is on a communication channel. Works on a
// full line (timestamp and all) or a timestamp-stripped body.
func IsChat(line string) bool {
	for _, m := range chatMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// Scrub copies r to w line by line, dropping every chat line, and reports how
// many it removed. A trailing newline is written after each kept line.
func Scrub(w io.Writer, r io.Reader) (removed int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	bw := bufio.NewWriter(w)
	for sc.Scan() {
		line := sc.Text()
		if IsChat(line) {
			removed++
			continue
		}
		if _, err = bw.WriteString(line + "\n"); err != nil {
			return removed, err
		}
	}
	if err = sc.Err(); err != nil {
		return removed, err
	}
	return removed, bw.Flush()
}
