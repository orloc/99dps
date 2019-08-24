package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"sync"
	"github.com/imdario/mergo"
	"99dps/util"
	"github.com/hpcloud/tail"
)

type DmgParser struct {
	workingString string
}

const COMBAT_VERB_STRING = "gores|gore|healed|heal|claws|claw|punches|punch|kicks|kick|bites|bite|mauls|maul|slashes|slash|slices|slice|strikes|strike|stings|sting|pierces|pierce|bashes|bash|hits|hit|backstabs|backstab|crushes|crush|non-melee"

func DoParse(t *tail.Tail, session *CombatSession, mutex *sync.RWMutex) {
	p := DmgParser{}

	if !session.IsStarted() {
		session.Init()
	}

	for line := range t.Lines {
		if p.hasDamage(line.Text) {
			dmgSet := p.parseDamage(line.Text)
			session.AdjustDamage(&dmgSet, mutex)
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

func (parser *DmgParser) parseDamage(inputString string) (set DamageSet) {
	parser.workingString = inputString
	var wg sync.WaitGroup
	c := make(chan DamageSet, 4)
	wg.Add(4)

	go func(){
		wg.Wait()
		close(c)
	}()

	go parser.getTime(c, &wg)
	go parser.getDealer(c, &wg)
	go parser.getDamage(c, &wg)
	go parser.getTarget(c, &wg)

	result := DamageSet{}

	for msg := range c {
		if err := mergo.Merge(&result, msg); err != nil {
			// RIP
			fmt.Errorf("ERROR: %s", err)
			continue
		}
	}

	return result
}

func (parser *DmgParser) getTime(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	t, err := time.Parse(time.ANSIC, parser.workingString[1:25])
	util.CheckErr(err)

	c <- DamageSet{ ActionTime: t.Unix()}
}

func (parser *DmgParser) getDamage(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	damagePattern := regexp.MustCompile(`[0-9]+`)
	match := damagePattern.FindString(parser.workingString[27:])

	dmg, err := strconv.Atoi(match)
	util.CheckErr(err)

	c <- DamageSet{ Dmg: dmg}
}

func (parser *DmgParser) getDealer(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	dealerPattern := regexp.MustCompile(fmt.Sprintf("^(.*(?:(%s)))", COMBAT_VERB_STRING))
	match := dealerPattern.FindString(parser.workingString[27:])

	replacePattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))

	replaced := replacePattern.ReplaceAll([]byte(match), []byte(""))

	c <- DamageSet{ Dealer: strings.Trim(string(replaced), " ")}
}

func (parser *DmgParser) getTarget(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	targetPattern := regexp.MustCompile(`.*(?:for)`)
	indxPattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))
	replacePattern := regexp.MustCompile(`for`)

	match := targetPattern.FindString(parser.workingString[27:])

	indx := indxPattern.FindIndex([]byte(match))

	replaced := replacePattern.ReplaceAll([]byte(match[indx[1]:]), []byte(" "))
	s := strings.Trim(string(replaced), " ")

	if strings.Contains(s, "YOU") {
		s = "YOU"
	}

	if strings.Contains(s, "non-melee") {
		s = "non-melee"
	}

	c <- DamageSet{ Target: strings.Trim(string(replaced), " ")}
}
