package main

import (
	"github.com/hpcloud/tail"
	"log"
	"99dps/parser"
	"fmt"
)

const intro = `
=================================
= 99DPS a CLI p99 damage parser =
=================================

The parser will automatically find the most recently used log file 
and begin parsing that.

Commands:
- 'print' : dislays current user DPS

`

const EVENT_DISPLAY = "do_print"

func main() {

	fmt.Println(intro)
	activeFile := loadFile()

	inputChan := make(chan string)
	session := parser.CombatSession{}

	defer close(inputChan)

	go scanInput(inputChan)
	go doParse(activeFile, &session)

	for msg := range inputChan {
		switch msg {
		case EVENT_DISPLAY:
			session.Display()
		}
	}
}

func doParse(t *tail.Tail, session *parser.CombatSession) {
	p := parser.DmgParser{}

	if !session.IsStarted() {
		session.Init()
	}

	for line := range t.Lines {
		if p.HasDamage(line.Text) {
			dmgSet := p.ParseDamage(line.Text)
			session.AdjustDamage(dmgSet)
		}
	}
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
