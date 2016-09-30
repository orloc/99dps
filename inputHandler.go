package main

import (
	"bufio"
	"log"
	"os"
)

func scanInput(c chan string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {

		switch scanner.Text() {
		case "print":
			log.Println("you got things")
			break
		case "quit":
			log.Println("quiting bitches")
			break
		case "start":
			log.Println("done started")
			break
		case "stop":
			log.Println("get rekt")
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
