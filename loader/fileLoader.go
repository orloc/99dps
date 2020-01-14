package loader

import (
	"99dps/common"
	"99dps/sorts"
	"fmt"
	"github.com/hpcloud/tail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// @TODO make me config
const eqLogDir = "/home/orloc/.wine/drive_c/everquest/Logs"

func LoadFile() *tail.Tail {
	fname := getLastActiveFile()

	//	startSpot := tail.SeekInfo{0, os.SEEK_END}
	t, err := tail.TailFile(fname, tail.Config{
		//		Location: &startSpot,
		Follow: true,
	})
	common.CheckErr(err)

	return t
}

func getLastActiveFile() string {
	var validCharFile = regexp.MustCompile(`^.*eqlog_.*.txt$`)

	dir, err := filepath.Abs(eqLogDir)
	common.CheckErr(err)
	var fileList []os.FileInfo

	err = filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if validCharFile.MatchString(path) {
			fileList = append(fileList, f)
		}
		return nil
	})

	common.CheckErr(err)
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
