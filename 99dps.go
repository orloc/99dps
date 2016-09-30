package main

import (
	"github.com/hpcloud/tail"
	"log"
)

func main() {
	t := loadFile()

	inputChan := make(chan string)

	go scanInput(inputChan)
	go doParse(t)

	for {
		newInput := <-inputChan
		log.Println(newInput)
	}

}

func doParse(t *tail.Tail) {
	parser := DmgParser{}
	session := CombatSession{}

	for line := range t.Lines {
		if parser.HasDamage(line.Text) {
			if session.isStarted() {
				log.Println("hi")
			} else {
				dmgSet := parser.ParseDamage(line.Text)
				_ = dmgSet
			}

			// if the session is within an accepted interval
			// use the old session - otherwise store and add to the new session
			// get target damager and damage with time

		}
	}
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
