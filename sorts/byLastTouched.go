package sorts

import (
	"os"
)

type ByLastTouched []os.FileInfo

func (s ByLastTouched) Len() int {
	return len(s)
}

func (s ByLastTouched) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByLastTouched) Less(i, j int) bool {
	return s[i].ModTime().After(s[j].ModTime())
}
