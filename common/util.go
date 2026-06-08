package common

import (
	"log"
)

// CheckErr aborts the program when err is non-nil. Intended for unrecoverable
// startup failures (gui init, log discovery) where there's nothing to fall back
// to.
func CheckErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
