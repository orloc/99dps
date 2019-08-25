package main

import (
	"99dps/input"
	"99dps/loader"
	"99dps/parser"
	"99dps/session"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var rwLock = sync.RWMutex{}

func main() {
	inputChan := make(chan string)
	defer close(inputChan)

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\rShutting down..")
		os.Exit(0)
	}()

	input.Help()

	activeFile := loader.LoadFile()

	sm := session.SessionManager{}

	go parser.DoParse(activeFile, &sm, &rwLock)
	go input.ScanInput(inputChan)

	input.HandleInput(inputChan, &sm, &rwLock)
}
