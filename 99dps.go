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
	"time"
)

const eqLogDir = "/home/orloc/WineDirs/eq/drive_c/everquest/Logs"

func main() {
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

	fname := getFilePath(topFile)
	file, err := os.Open(fname)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	c := time.Tick(1 * time.Second)
	for _ = range c {
		readFile(fname, file)
	}

	/*
		fileHandle, err := os.Open(getFilePath(topFile))
		checkErr(err)

		scanner := bufio.NewScanner(fileHandle)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	*/
}

func readFile(fname string, file *os.File) {
	buf := make([]byte, 100)
	stat, err := os.Stat(fname)
	start := stat.Size() - 100
	_, err = file.ReadAt(buf, start)
	if err == nil {
		fmt.Printf("%s\n", buf)
	}

}
func getFilePath(f os.FileInfo) string {
	return eqLogDir + "/" + f.Name()
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
