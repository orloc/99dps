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
	sm := session.SessionManager{Mutex: &rwLock }

	a := app.New(&sm, &rwLock)

	go parser.DoParse(activeFile, &sm, &rwLock)
	go a.Sync()

	a.Loop()
}

