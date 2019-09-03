package main

import (
	"99dps/app"
	"99dps/loader"
	"99dps/parser"
	"99dps/session"
	"sync"
)

var rwLock = sync.RWMutex{}

func main() {
	activeFile := loader.LoadFile()
	sm := session.SessionManager{Mutex: &rwLock}

	a := app.New(&sm, &rwLock)

	go parser.DoParse(activeFile, &sm, &rwLock)
	go a.Sync()

	a.Loop()
}
