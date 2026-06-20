// Command gen scrapes the Project 1999 wiki's Category:Spells into the committed
// internal/spellwiki/spelldata.json — the authoritative, offline P99 spell set the
// app validates casts against. It's invoked by `go generate ./internal/spellwiki`
// (a maintainer task, run occasionally; the output is committed). It is NOT part
// of the app binary — the app imports only the pure `spellwiki` package.
//
// Pipeline: enumerate Category:Spells via the MediaWiki API → fetch each page's
// raw wikitext → spellwiki.Parse → key by spellwiki.Key. It VALIDATES the result
// (a sane count + known anchor spells present) before writing, so a broken scrape
// can't clobber a good dataset.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"99dps/internal/spellwiki"
)

func main() {
	out := flag.String("out", "spelldata.json", "output path for the dataset")
	base := flag.String("base", "https://wiki.project1999.com", "wiki origin")
	delay := flag.Duration("delay", 120*time.Millisecond, "pause between page fetches (be polite)")
	limit := flag.Int("limit", 0, "stop after N spells (0 = all; for testing the pipeline)")
	title := flag.String("title", "", "debug: fetch+parse a single title and print it, then exit")
	flag.Parse()

	c := &scraper{base: *base, http: &http.Client{Timeout: 20 * time.Second}, delay: *delay}

	if *title != "" {
		sp, ok, err := c.fetchSpell(*title)
		fmt.Printf("title=%q ok=%v err=%v\n  name=%q classes=%v detrimental=%v dur=%q\n",
			*title, ok, err, sp.Name, sp.Classes, sp.Detrimental, sp.Duration)
		return
	}

	titles, err := c.categoryMembers("Category:Spells")
	if err != nil {
		fatal("enumerate Category:Spells: %v", err)
	}
	fmt.Fprintf(os.Stderr, "enumerated %d spell pages\n", len(titles))
	if *limit > 0 && *limit < len(titles) {
		titles = titles[:*limit]
	}

	spells := map[string]spellwiki.Spell{}
	var skipped int
	for i, title := range titles {
		sp, ok, err := c.fetchSpell(title)
		if err != nil {
			fatal("fetch %q: %v", title, err) // a transport error is fatal — don't write a partial set
		}
		if !ok {
			skipped++
			continue // not a spell page (a redirect/stub) — skip, don't poison the set
		}
		spells[spellwiki.Key(title)] = sp
		if (i+1)%200 == 0 {
			fmt.Fprintf(os.Stderr, "  …%d/%d\n", i+1, len(titles))
		}
		time.Sleep(c.delay)
	}
	fmt.Fprintf(os.Stderr, "parsed %d spells (%d skipped)\n", len(spells), skipped)

	if err := validate(spells, *limit); err != nil {
		fatal("validation failed, NOT writing %s: %v", *out, err)
	}

	doc := struct {
		Generated string                     `json:"generated"`
		Source    string                     `json:"source"`
		Spells    map[string]spellwiki.Spell `json:"spells"`
	}{
		Generated: time.Now().UTC().Format("2006-01-02"),
		Source:    *base + "/Category:Spells",
		Spells:    spells,
	}
	b, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		fatal("marshal: %v", err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fatal("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d spells, %d bytes)\n", *out, len(spells), len(b))
}

// validate guards against a broken scrape clobbering a good dataset: require a
// plausible count and that well-known classic spells (one per archetype) parsed
// with the expected caster. Skipped for a -limit test run.
func validate(spells map[string]spellwiki.Spell, limit int) error {
	if limit > 0 {
		return nil // a partial test run — skip the full-set assertions
	}
	if len(spells) < 1000 {
		return fmt.Errorf("only %d spells parsed, expected ~1500", len(spells))
	}
	anchors := map[string]string{ // spell -> a class that must be able to cast it
		"Spirit of Wolf": "Shaman", "Complete Healing": "Cleric", "Mesmerize": "Enchanter",
		"Ice Comet": "Wizard", "Cripple": "Shaman",
		"Complete Heal": "Cleric", // a disambig page — must resolve via the follow hop
	}
	for name, class := range anchors {
		sp, ok := spells[spellwiki.Key(name)]
		if !ok {
			return fmt.Errorf("anchor spell %q missing from the scrape", name)
		}
		if _, ok := sp.CastableBy(class); !ok {
			return fmt.Errorf("anchor %q should be castable by %s but isn't", name, class)
		}
	}
	return nil
}

// scraper holds the wiki HTTP config.
type scraper struct {
	base  string
	http  *http.Client
	delay time.Duration
}

// categoryMembers returns every page title in a category, following the API's
// continuation. API list calls are cheap (500/page) and don't need a delay.
func (s *scraper) categoryMembers(cat string) ([]string, error) {
	var titles []string
	cont := ""
	for {
		u := fmt.Sprintf("%s/api.php?action=query&list=categorymembers&cmtitle=%s&cmtype=page&cmlimit=500&format=json",
			s.base, url.QueryEscape(cat))
		if cont != "" {
			u += "&cmcontinue=" + url.QueryEscape(cont)
		}
		body, status, err := s.get(u)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("API HTTP %d", status)
		}
		var r struct {
			Query struct {
				CategoryMembers []struct {
					Title string `json:"title"`
				} `json:"categorymembers"`
			} `json:"query"`
			QueryContinue struct {
				CategoryMembers struct {
					CmContinue string `json:"cmcontinue"`
				} `json:"categorymembers"`
			} `json:"query-continue"`
			Continue struct {
				CmContinue string `json:"cmcontinue"`
			} `json:"continue"`
		}
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			return nil, err
		}
		for _, m := range r.Query.CategoryMembers {
			titles = append(titles, m.Title)
		}
		cont = r.Continue.CmContinue
		if cont == "" {
			cont = r.QueryContinue.CategoryMembers.CmContinue
		}
		if cont == "" {
			break
		}
	}
	sort.Strings(titles)
	return titles, nil
}

// followRe pulls the [[target]] out of a redirect ("#REDIRECT [[X]]") or a spell
// disambiguation ("{{Disambig3|The player cast version [[X]]}}"). Both point a
// client-facing name (e.g. "Complete Heal") at the real player-spell page (e.g.
// "Complete Healing"); following them keeps those names from being false-flagged.
var followRe = regexp.MustCompile(`(?is)(?:#redirect|\{\{disambig\d?\b)[^\[]*\[\[([^\]|#]+)`)

// fetchSpell GETs a title's raw wikitext and parses it, following ONE redirect or
// disambiguation hop to the real spell page. ok=false for a page that still isn't
// a spell template after the hop (a true stub) — the caller skips those.
func (s *scraper) fetchSpell(title string) (spellwiki.Spell, bool, error) {
	body, status, err := s.getRaw(title)
	if err != nil || status == http.StatusNotFound {
		return spellwiki.Spell{}, false, err
	}
	if status != http.StatusOK {
		return spellwiki.Spell{}, false, fmt.Errorf("HTTP %d", status)
	}
	sp, ok := spellwiki.Parse(body)
	if ok && len(sp.Classes) > 0 {
		return sp, true, nil // a real player-castable spell page
	}
	// Either not a spell page, or a CLASSLESS stub: a disambiguation page (e.g.
	// "Complete Heal") embeds the NPC {{Spellpage}} (no player classes) AND a
	// "{{Disambig3|… [[Complete Healing]]}}" pointer to the player version; a
	// redirect is similar. If there's a follow target, the target is the real
	// player spell — fetch it and key the original (client-facing) name to it.
	if m := followRe.FindStringSubmatch(body); m != nil {
		time.Sleep(s.delay)
		body, status, err = s.getRaw(strings.TrimSpace(m[1]))
		if err != nil || status != http.StatusOK {
			return spellwiki.Spell{}, false, err
		}
		if sp2, ok2 := spellwiki.Parse(body); ok2 {
			return sp2, true, nil
		}
	}
	return sp, ok, nil // no target: keep what we parsed (a genuine NPC/classless spell) or skip
}

// getRaw fetches a title's raw wikitext (spaces → underscores, as titles require).
func (s *scraper) getRaw(title string) (string, int, error) {
	u := fmt.Sprintf("%s/index.php?title=%s&action=raw", s.base, url.QueryEscape(strings.ReplaceAll(title, " ", "_")))
	return s.get(u)
}

func (s *scraper) get(u string) (string, int, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "99dps spelldata generator (github.com/99dps)")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", 0, err
	}
	return string(body), resp.StatusCode, nil
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gen: "+format+"\n", a...)
	os.Exit(1)
}
