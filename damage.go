package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

/*

	//[Wed Sep 21 01:17:52 2016] Higgy slashes an elemental warrior for 8 points of damage.
type CombatSession struct {
	DmgParser *DamageList
	Start      time.Time
	End        time.Time
}
*/

type DmgParser struct {
	workingString string
	Index         []int
	Names         []string
	TotalDamage   []int
	SpellsCast    []int
	Times         []time.Time
}

func (parser *DmgParser) HasDamage(inputString string) bool {
	pattern := regexp.MustCompile(`^(\[.*\])(.*(?:(points of damage)))`)
	return pattern.Match([]byte(inputString))
}

func (parser *DmgParser) ParseDamage(inputString string) {
	c := make(chan string)
	ct := make(chan time.Time)
	parser.workingString = inputString
	go parser.getTime(ct)
	go parser.getDealer(c)
	go parser.getDamage(c)
	go parser.getTarget(c)

	time, dealer, damage, target := <-ct, <-c, <-c, <-c

	fmt.Println(target)

}

func (parser *DmgParser) getTime(c chan time.Time) {
	time, err := time.Parse(time.ANSIC, parser.workingString[1:25])
	checkErr(err)
	c <- time
}

func (parser *DmgParser) getTarget(c chan string) {
	targetPattern := regexp.MustCompile(`.*(?:for)`)
	indxPattern := regexp.MustCompile(`(punches|kicks|slashes|bites|pierces|bashes|hits|backstabs|crushes)`)
	match := targetPattern.FindString(parser.workingString[27:])

	indx := indxPattern.FindIndex([]byte(match))
	fmt.Printf("%v", indx, match)

	c <- match[indx[1]-1:]
}

func (parser *DmgParser) getDamage(c chan string) {
	damagePattern := regexp.MustCompile(`[0-9]+`)
	match := damagePattern.FindString(parser.workingString[27:])
	c <- match
}

func (parser *DmgParser) getDealer(c chan string) {
	dealerPattern := regexp.MustCompile(`^(.*(?:(punches|kicks|bites|slashes|pierces|bashes|hits|backstabs|crushes)))`)
	match := dealerPattern.FindString(parser.workingString[27:])
	replacePattern := regexp.MustCompile(`(punches|kicks|slashes|bites|pierces|bashes|hits|backstabs|crushes)`)

	replaced := replacePattern.ReplaceAll([]byte(match), []byte(""))

	c <- strings.Trim(string(replaced), " ")
}
