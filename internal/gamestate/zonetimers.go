package gamestate

import "strings"

// zoneRespawnSec maps a P99 zone to its default zone-wide respawn time in
// seconds. Source: docs/zone-spawn-timers.md (transcribed from the P99 wiki).
// These are zone DEFAULTS — named mobs, placeholders, and special mob types
// often differ; for zones the wiki lists with several sub-timers, the primary
// (trash) value is used here. Keys are normalized (lowercase, no leading "the")
// to match a "You have entered X" zone-in line via normalizeZone.
var zoneRespawnSec = map[string]int{
	// Antonica — outdoors
	"beholder's maze": 360, "east commonlands": 400, "eastern karana": 400,
	"erud's crossing": 400, "everfrost": 400, "highpass hold": 300,
	"innothule swamp": 400, "kithicor forest": 400, "lake rathetear": 400,
	"lavastorm mountains": 400, "misty thicket": 400, "nektulos forest": 400,
	"northern desert of ro": 400, "northern karana": 400, "oasis of marr": 990,
	"ocean of tears": 360, "qeynos hills": 400, "rathe mountains": 400,
	"southern desert of ro": 400, "southern karana": 360, "feerrott": 400,
	"west commonlands": 400, "western karana": 400,

	// Antonica — cities
	"grobb": 1440, "halas": 1440, "neriak": 1440, "freeport": 1440,
	"qeynos": 400, "oggok": 1440, "rivervale": 1320, "surefall glade": 400,

	// Antonica — dungeons
	"befallen": 1140, "blackburrow": 1320, "cazic thule": 1320,
	"clan runnyeye": 1320, "high keep": 600, "lower guk": 1680,
	"nagafen's lair": 1320, "najena": 1110, "permafrost caverns": 1320,
	"qeynos catacombs": 1440, "solusek's eye": 1080, "splitpaw lair": 1320,
	"temple of solusek ro": 300, "upper guk": 990,

	// Odus
	"erudin": 400, "erudin palace": 1500, "paineel": 630, "kerra island": 1065,
	"toxxulia forest": 400, "hole": 1290, "stonebrunt mountains": 670,
	"warrens": 400,

	// Faydwer
	"ak'anon": 400, "felwithe": 1440, "kaladim": 400,
	"butcherblock mountains": 600, "greater faydark": 425, "lesser faydark": 390,
	"steamfont mountains": 400, "crushbone": 540, "kedge keep": 1320,
	"mistmoore castle": 1320, "estate of unrest": 1320,

	// Kunark — outdoors / city
	"burning wood": 400, "dreadlands": 400, "field of bone": 400,
	"firiona vie": 400, "frontier mountains": 400, "lake of ill omen": 400,
	"overthere": 400, "skyfire mountains": 780, "swamp of no hope": 400,
	"timorous deep": 720, "trakanon's teeth": 400, "warsliks woods": 400,
	"cabilis": 400,

	// Kunark — dungeons
	"chardok": 1080, "city of mist": 1320, "dalnir": 720, "howling stones": 1230,
	"kaesora": 1080, "karnor's castle": 1620, "kurn's tower": 1100,
	"mines of nurga": 1230, "old sebilis": 1620, "temple of droga": 1230,

	// Velious — outdoors
	"cobalt scar": 1200, "eastern wastes": 400, "great divide": 640,
	"iceclad ocean": 400, "wakening lands": 400,

	// Velious — cities
	"icewell keep": 1260, "kael drakkal": 1680, "skyshrine": 1800,
	"thurgadin": 420,

	// Velious — dungeons
	"crystal caverns": 885, "dragon necropolis": 1620, "siren's grotto": 1680,
	"sleeper's tomb": 28800, "temple of veeshan": 4320,
	"tower of frozen shadow": 1200, "velketor's labyrinth": 1970,

	// Planes
	"plane of fear": 28800, "plane of hate": 28800, "plane of sky": 28800,
	"plane of growth": 43200, "plane of mischief": 4210,
}

// ZoneRespawn returns the default respawn (seconds) for a zone name as it
// appears in a "You have entered X" line, and whether it's known.
func ZoneRespawn(zone string) (int, bool) {
	s, ok := zoneRespawnSec[normalizeZone(zone)]
	return s, ok
}

// normalizeZone lowercases, trims a trailing period, and drops a leading "the "
// so a zone-in line matches the table keys.
func normalizeZone(z string) string {
	z = strings.ToLower(strings.TrimSpace(z))
	z = strings.TrimSuffix(z, ".")
	z = strings.TrimPrefix(z, "the ")
	return z
}
