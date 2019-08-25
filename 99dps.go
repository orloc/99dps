package main

import (
	"sync"
	"99dps/loader"
	"99dps/session"
	"99dps/parser"
	"99dps/app"
)

var rwLock = sync.RWMutex{}

func main() {


	activeFile := loader.LoadFile()
	sm := session.SessionManager{}

	a := app.New()

	go parser.DoParse(activeFile, &sm, &rwLock)

	a.Loop()

	/*
	inputChan := make(chan string)
	defer close(inputChan)

	input.Help()

	go input.ScanInput(inputChan)

	input.HandleInput(inputChan, &sm, &rwLock)
	*/
}

