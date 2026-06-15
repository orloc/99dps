package sanitize

import (
	"strings"
	"testing"
)

const ts = "[Sat Jun 14 21:00:00 2026] "

// TestIsChat: every communication channel is detected (real log formats), and
// combat/mechanics lines are kept.
func TestIsChat(t *testing.T) {
	chat := []string{
		"Evisnes tells you, 'Mantle of Mirth'",
		"You told Vesperinia '[queued], okes im a bard'", // outgoing tell, no comma
		"Buckeboom tells the guild, 'Statue just spawn?'",
		"You say to your guild, 'pe wpew'",
		"Feressa tells the group, 'ty'",
		"You tell your party, 'thanks again'",
		"Swiftblade tells the raid, 'inc adds'",
		"You tell the raid, 'pull on my mark'",
		"Ruluwen says out of character, 'where did everyone go'",
		"You say out of character, 'lfg sebilis'",
		"Natimi auctions, 'WTS Large Soiled Bag 800p'",
		"You auction, 'WTS Jaundiced Bone Bracer 15k PST'",
		"The Progenitor shouts, 'One must attend to a distraction.'",
		"You shout, 'all hail muujo'",
		"Sugarsnap says, 'need a ride?'",
		"You say, 'hello'",
		"Gorignak says, 'My leader is Iznoa.'", // pet say — also a chat channel
	}
	for _, body := range chat {
		if !IsChat(ts + body) {
			t.Errorf("should be flagged as chat: %q", body)
		}
	}

	keep := []string{
		"You slash a sand giant for 100 points of damage.",
		"a sand giant has been slain by Iznoa!",
		"a sand giant's body convulses with the poison.", // spell-landing emote
		"You have entered Sebilis.",
		"You gain party experience!!",
		"**A Magic Die is rolled by Kelkix.",
	}
	for _, body := range keep {
		if IsChat(ts + body) {
			t.Errorf("should NOT be flagged as chat: %q", body)
		}
	}
}

// TestScrub: chat lines are dropped, everything else is preserved verbatim, and
// the removed count is right.
func TestScrub(t *testing.T) {
	in := strings.Join([]string{
		ts + "You slash a sand giant for 100 points of damage.",
		ts + "Buckeboom tells the guild, 'secret raid strat'",
		ts + "a sand giant has been slain by Iznoa!",
		ts + "Evisnes tells you, 'wtb that drop'",
		ts + "You have entered Sebilis.",
	}, "\n") + "\n"

	var out strings.Builder
	removed, err := Scrub(&out, strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	got := out.String()
	for _, leak := range []string{"secret raid strat", "wtb that drop", "tells", "told"} {
		if strings.Contains(got, leak) {
			t.Errorf("scrubbed output still leaks %q:\n%s", leak, got)
		}
	}
	for _, keep := range []string{"100 points of damage", "has been slain", "entered Sebilis"} {
		if !strings.Contains(got, keep) {
			t.Errorf("scrubbed output dropped a kept line %q:\n%s", keep, got)
		}
	}
}
