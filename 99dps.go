package main

import (
	_ "bufio"
	"fmt"
	"github.com/hpcloud/tail"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const eqLogDir = "/home/orloc/WineDirs/eq/drive_c/everquest/Logs"

func main() {

	fname := getLastActiveFile()

	//startSpot := tail.SeekInfo{0, os.SEEK_END}
	t, err := tail.TailFile(fname, tail.Config{
		//	Location: &startSpot,
		Follow: true,
	})

	checkErr(err)

	for line := range t.Lines {
		fmt.Println(line.Text)
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
