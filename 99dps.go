package main

import (
	"sync"
)

var rwLock = sync.RWMutex{}

func main() {
	launchCLI()
}

