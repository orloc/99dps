package input

import (
	"99dps/session"
	"bufio"
	"fmt"
	"log"
	"os"
	"sync"
)

const (
	EVENT_DISPLAY = "do_print"
	EVENT_CLEAR   = "do_clear"
	EVENT_HELP    = "do_help"
	EVENT_ALL     = "do_all"
	EVENT_QUIT    = "do_quit"
)

const intro = `
=================================
= 99DPS a CLI p99 damage parser =
=================================

The parser will automatically find the most recently used log file 
and begin parsing that.

Commands:
- (p)rint : displays current combat session
- (a)ll : displays all combat sessions
- (c)lear : deletes combat session records
- (h)elp : shows this menu
- (q)uit : exit

`

func HandleInput(inputChan chan string, sm *session.SessionManager, rwLock *sync.RWMutex) {
	for msg := range inputChan {
		switch msg {
		case EVENT_HELP:
			Help()
		case EVENT_CLEAR:
			sm.Clear(rwLock)
			fmt.Println("All data cleared...")
		case EVENT_ALL:
			sm.All(rwLock)
		case EVENT_QUIT:
			fmt.Println("Shutting down...")
			os.Exit(1)
		case EVENT_DISPLAY:
			sm.Display(rwLock)
		}
	}
}

func Help() {
	fmt.Println(intro)
}

func ScanInput(c chan string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "q":
			fallthrough
		case "quit":
			c <- EVENT_QUIT
		case "p":
			fallthrough
		case "print":
			c <- EVENT_DISPLAY
		case "c":
			fallthrough
		case "clear":
			c <- EVENT_CLEAR
		case "h":
			fallthrough
		case "help":
			c <- EVENT_HELP
		case "a":
			fallthrough
		case "all":
			c <- EVENT_ALL
		default:
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
