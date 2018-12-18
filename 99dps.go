package main

import (
	"github.com/hpcloud/tail"
	"log"
	"99dps/parser"
)

const EVENT_DISPLAY = "do_print"

func main() {
	activeFile := loadFile()

	inputChan := make(chan string)
	session := parser.CombatSession{}

	go scanInput(inputChan)
	go doParse(activeFile, &session)

	for {
		newInput := <-inputChan
		switch newInput {
		case EVENT_DISPLAY:
			session.Display()
			break
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
			//session.Display()
		}
	}
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
