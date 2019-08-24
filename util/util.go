package util

import "log"

func CheckErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
