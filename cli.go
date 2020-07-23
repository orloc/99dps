package main

import (
	"99dps/loader"
	"99dps/session"
	"99dps/parser"
	app "99dps/cli"
)

func launchCLI() {
	activeFile := loader.LoadFile()
	sm := session.SessionManager{Mutex: &rwLock}

	a := app.New(&sm, &rwLock)

	go parser.DoParse(activeFile, &sm, &rwLock)
	go a.Sync()

	a.Loop()
}
