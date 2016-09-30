package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DmgParser struct {
	workingString string
}

type DamageSet struct {
	actionTime time.Time
	dealer     string
	dmg        int
	target     string
}

const COMBAT_VERB_STRING = "healed|heal|claw|claws|punches|punch|kicks|kick|bites|bite|slashes|slash|stings|sting|pierces|pierce|bashes|bash|hits|crush|backstabs|backstab|crushes|crush|non-melee"

func (parser *DmgParser) HasDamage(inputString string) bool {
	pattern := regexp.MustCompile(`^(\[.*\])(.*(?:(points of damage)))`)
	taken := regexp.MustCompile(`^(You have taken)([0-9]*(?:(points of damage)))`)
	fmt.Println(pattern.Match([]byte(inputString)), !taken.Match([]byte(inputString)))
	return pattern.Match([]byte(inputString)) && !taken.Match([]byte(inputString))
}

func (parser *DmgParser) ParseDamage(inputString string) (set *DamageSet) {
	parser.workingString = inputString
	fmt.Printf("%v\n", inputString)
	c := make(chan string)
	ct := make(chan time.Time)

	defer close(c)
	defer close(ct)

	go parser.getTime(ct)
	go parser.getDealer(c)
	time, dealer := <-ct, <-c
	go parser.getDamage(c)
	damage := <-c
	go parser.getTarget(c)
	target := <-c

	dmg, err := strconv.Atoi(damage)
	checkErr(err)

	return &DamageSet{time, dealer, dmg, target}
}

func (parser *DmgParser) getTime(c chan time.Time) {
	time, err := time.Parse(time.ANSIC, parser.workingString[1:25])
	checkErr(err)
	c <- time
}

func (parser *DmgParser) getLineTime(input string) time.Time {
	time, err := time.Parse(time.ANSIC, input[1:25])
	checkErr(err)
	return time
}

func (parser *DmgParser) getDamage(c chan string) {
	damagePattern := regexp.MustCompile(`[0-9]+`)
	match := damagePattern.FindString(parser.workingString[27:])
	c <- match
}

func (parser *DmgParser) getDealer(c chan string) {
	dealerPattern := regexp.MustCompile(fmt.Sprintf("^(.*(?:(%s)))", COMBAT_VERB_STRING))
	match := dealerPattern.FindString(parser.workingString[27:])
	replacePattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))

	replaced := replacePattern.ReplaceAll([]byte(match), []byte(""))

	c <- strings.Trim(string(replaced), " ")
}

func (parser *DmgParser) getTarget(c chan string) {
	targetPattern := regexp.MustCompile(`.*(?:for)`)
	indxPattern := regexp.MustCompile(fmt.Sprintf("(%s)", COMBAT_VERB_STRING))
	replacePattern := regexp.MustCompile(`for`)

	match := targetPattern.FindString(parser.workingString[27:])
	indx := indxPattern.FindIndex([]byte(match))

	replaced := replacePattern.ReplaceAll([]byte(match[indx[1]:]), []byte(" "))

	c <- strings.Trim(string(replaced), " ")
}
