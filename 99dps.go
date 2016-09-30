package main

import (
	"fmt"
	"github.com/hpcloud/tail"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	_ "time"
)

const eqLogDir = "/home/orloc/WineDirs/eq/drive_c/everquest/Logs"

func main() {

	fname := getLastActiveFile()

	fmt.Println("Opening %s", fname)

	//startSpot := tail.SeekInfo{0, os.SEEK_END}
	t, err := tail.TailFile(fname, tail.Config{
		//	Location: &startSpot,
		Follow: true,
	})

	checkErr(err)
	parser := DmgParser{}
	session := CombatSession{}

	for line := range t.Lines {
		if parser.HasDamage(line.Text) {
			// if the session is within an accepted interval
			// use the old session - otherwise store and add to the new session
			// get target damager and damage with time
			dmgSet := parser.ParseDamage(line.Text)
			session.Add(dmgSet)

		}
	}
}

func getLastActiveFile() string {
	var validCharFile = regexp.MustCompile(`^.*eqlog_.*project1999.txt$`)

	dir, err := filepath.Abs(eqLogDir)
	checkErr(err)
	fileList := []os.FileInfo{}

	err = filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if validCharFile.MatchString(path) {
			fileList = append(fileList, f)
		}
		return nil
	})

	checkErr(err)
	sort.Sort(ByLastTouched(fileList))

	topFile := fileList[0]

	return getFilePath(topFile)
}

func getFilePath(f os.FileInfo) string {
	return eqLogDir + "/" + f.Name()
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
