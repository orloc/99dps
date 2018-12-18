package parser

import "log"

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
