package main

import (
	"99dps/parser"
	"fmt"
	"sync"
	"99dps/loader"
	"99dps/session"
)

var rwLock = sync.RWMutex{}

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
	activeFile := loader.LoadFile()

	inputChan := make(chan string)
	sm := session.SessionManager{}

	defer close(inputChan)

	go parser.DoParse(activeFile, &sm, &rwLock)
	go scanInput(inputChan)

	for msg := range inputChan {
		switch msg {
		case EVENT_DISPLAY:
			sm.Display(&rwLock)
		}
	}
}

