package main

import (
	"bufio"
	"fmt"
	"github.com/hpcloud/tail"
	"log"
	"os"
)

func main() {
	t := loadFile()

	inputChan := make(chan string)

	go scanInput(inputChan)
	go doParse(t)

	for {
		newInput := <-inputChan
		fmt.Println(newInput)
	}

}

func doParse(t *tail.Tail) {
	parser := DmgParser{}
	session := CombatSession{}

	for line := range t.Lines {
		if parser.HasDamage(line.Text) {
			if session.isStarted() {
				fmt.Println("hi")
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

func scanInput(c chan string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {

		switch scanner.Text() {
		case "print":
			fmt.Println("you got things")
			break
		case "quit":
			fmt.Println("quiting bitches")
			break
		case "start":
			fmt.Println("done started")
			break
		case "stop":
			fmt.Println("get rekt")
			break
		default:
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
