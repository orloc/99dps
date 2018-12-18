package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"sync"
)

type DmgParser struct {
	workingString string
}

type DamageSet struct {
	actionTime int64
	dealer     string
	dmg        int
	target     string
}

const COMBAT_VERB_STRING = "gore|gores|healed|heal|claw|claws|punches|punch|kicks|kick|bites|bite|maul|mauls|slashes|slash|slice|slices|strike|strikes|slash|stings|sting|pierces|pierce|bashes|bash|hit|hits|backstabs|backstab|crushes|crush|non-melee"

func (parser *DmgParser) HasDamage(inputString string) bool {
	pattern := regexp.MustCompile(`^(\[.*\])(.*(?:(points of damage)))`)
	// we should handle this form of damage but atm it will break things
	taken := regexp.MustCompile(`(You have taken)(.*(?:(points of damage)))`)
	return pattern.Match([]byte(inputString)) && !taken.Match([]byte(inputString))
}

func (parser *DmgParser) ParseDamage(inputString string) (set *DamageSet) {
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
		if msg.target != "" {
			result.target = msg.target
		}

		if msg.dealer != "" {
			result.dealer = msg.dealer
		}

		if msg.dmg != 0 {
			result.dmg = msg.dmg
		}

		if msg.actionTime != 0 {
			result.actionTime = msg.actionTime
		}
	}


	return &result
}

func (parser *DmgParser) getTime(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	t, err := time.Parse(time.ANSIC, parser.workingString[1:25])
	checkErr(err)


	c <- DamageSet{ actionTime: t.Unix()}
}

func (parser *DmgParser) getLineTime(input string) time.Time {
	t, err := time.Parse(time.ANSIC, input[1:25])
	checkErr(err)
	return t
}

func (parser *DmgParser) getDamage(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	damagePattern := regexp.MustCompile(`[0-9]+`)
	match := damagePattern.FindString(parser.workingString[27:])

	dmg, err := strconv.Atoi(match)
	checkErr(err)

	c <- DamageSet{ dmg: dmg}
}

func (parser *DmgParser) getDealer(c chan DamageSet, group *sync.WaitGroup) {
	defer group.Done()
	dealerPattern := regexp.MustCompile(fmt.Sprintf("^(.*(?:(%s)))", COMBAT_VERB_STRING))
	match := dealerPattern.FindString(parser.workingString[27:])
	replacePattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))

	replaced := replacePattern.ReplaceAll([]byte(match), []byte(""))

	c <- DamageSet{ dealer: strings.Trim(string(replaced), " ")}
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

	fmt.Println(s)

	c <- DamageSet{target: strings.Trim(string(replaced), " ")}
}
