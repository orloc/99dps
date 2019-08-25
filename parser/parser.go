package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"sync"
	"github.com/imdario/mergo"
	"99dps/common"
	"github.com/hpcloud/tail"
	"99dps/session"
)

type ParseMessage struct {
	Res common.DamageSet
	Err error
}

type DmgParser struct {
	workingString string
}

const COMBAT_VERB_STRING = "gores|gore|claws|claw|punches|punch|kicks|kick|bites|bite|mauls|maul|slashes|slash|slices|slice|strikes|strike|stings|sting|pierces|pierce|bashes|bash|hits|hit|backstabs|backstab|crushes|crush"
const HEAL_VERB_STRING = "healed|heal"
const MAGIC_VERB_STRING = "non-melee"

const LOG_TS_INDEX_END = 25
const LOG_SUBJECT_INDEX_START = 27

func DoParse(t *tail.Tail, manager *session.SessionManager, mutex *sync.RWMutex) {
	p := DmgParser{}

	for line := range t.Lines {
		if p.hasDamage(line.Text) {
			dmgSet, err := p.parseDamage(line.Text)
			s := manager.GetActiveSession(dmgSet)
			if err != nil {
				continue
			}

			s.AdjustDamage(dmgSet, mutex)
		}
	}
}

func (parser *DmgParser) hasDamage(inputString string) bool {
	pattern := regexp.MustCompile(`^(\[.*\])(.*(?:(points of damage)))`)
	// we should handle this form of damage but atm it will break things -
	// spell damage will be hard to attribute as messages are not consistent
	taken := regexp.MustCompile(`(You have taken)(.*(?:(points of damage)))`)
	return pattern.Match([]byte(inputString)) && !taken.Match([]byte(inputString))
}

func (parser *DmgParser) parseDamage(inputString string) (*common.DamageSet, error) {
	parser.workingString = inputString
	var wg sync.WaitGroup
	c := make(chan ParseMessage, 4)
	wg.Add(4)

	go func(){
		wg.Wait()
		close(c)
	}()

	go parser.getTime(c, &wg)
	go parser.getDealer(c, &wg)
	go parser.getDamage(c, &wg)
	go parser.getTarget(c, &wg)

	result := common.DamageSet{}

	for msg := range c {
		if msg.Err != nil {
			return nil, msg.Err
		}

		if err := mergo.Merge(&result, msg.Res); err != nil {
			return nil, err
		}
	}

	return &result, nil
}

func (parser *DmgParser) getTime(c chan <- ParseMessage, group *sync.WaitGroup) {
	defer group.Done()
	t, err := time.Parse(time.ANSIC, parser.workingString[1:LOG_TS_INDEX_END])
	if err != nil {
		c <- ParseMessage{ Err: err }
		return
	}

	c <- ParseMessage{ Res: common.DamageSet{ ActionTime: t.Unix()} }
}

func (parser *DmgParser) getDamage(c chan <- ParseMessage, group *sync.WaitGroup) {
	defer group.Done()
	damagePattern := regexp.MustCompile(`[0-9]+`)
	match := damagePattern.FindString(parser.workingString[LOG_SUBJECT_INDEX_START:])

	dmg, err := strconv.Atoi(match)
	if err != nil {
		c <- ParseMessage{ Err: err }
		return
	}

	c <- ParseMessage{ Res: common.DamageSet{ Dmg: dmg} }
}

func (parser *DmgParser) getDealer(c chan <- ParseMessage, group *sync.WaitGroup) {
	defer group.Done()
	dealerPattern := regexp.MustCompile(fmt.Sprintf("^(.*(?:(%s)))", COMBAT_VERB_STRING))
	match := dealerPattern.FindString(parser.workingString[LOG_SUBJECT_INDEX_START:])

	replacePattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))

	replaced := replacePattern.ReplaceAll([]byte(match), []byte(""))

	c <- ParseMessage{ Res: common.DamageSet{ Dealer: strings.Trim(string(replaced), " ")} }
}

func (parser *DmgParser) getTarget(c chan <- ParseMessage, group *sync.WaitGroup) {
	defer group.Done()
	targetPattern := regexp.MustCompile(`.*(?:for)`)
	indxPattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))
	replacePattern := regexp.MustCompile(`for`)


	match := targetPattern.FindString(parser.workingString[LOG_SUBJECT_INDEX_START:])

	indx := indxPattern.FindIndex([]byte(match))

	// not a damage string - branch to other combat strings or determine hirer in call chain
	if len(indx) == 0 {
		c <- ParseMessage{ Err: fmt.Errorf("warning - not a damage string") }
		return
	}

	replaced := replacePattern.ReplaceAll([]byte(match[indx[1]:]), []byte(" "))
	s := strings.Trim(string(replaced), " ")

	if strings.Contains(s, "YOU") {
		s = "YOU"
	}

	if strings.Contains(s, "non-melee") {
		s = "non-melee"
	}

	c <- ParseMessage{ Res: common.DamageSet{ Target: strings.Trim(string(replaced), " ")} }
}
