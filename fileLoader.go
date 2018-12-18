package main

import (
	"github.com/hpcloud/tail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"fmt"
	"99dps/sorts"
)

const eqLogDir = "/home/orloc/.wine/drive_c/everquest/Logs"

func loadFile() *tail.Tail {
	fname := getLastActiveFile()

	//	startSpot := tail.SeekInfo{0, os.SEEK_END}
	t, err := tail.TailFile(fname, tail.Config{
		//		Location: &startSpot,
		Follow: true,
	})
	checkErr(err)

	return t
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
	sort.Sort(sorts.ByLastTouched(fileList))

	if len(fileList) == 0 {
		panic(fmt.Sprintf("found no files in %s", eqLogDir))
	}

	topFile := fileList[0]

	return getFilePath(topFile)
}

func getFilePath(f os.FileInfo) string {
	return eqLogDir + "/" + f.Name()
}
