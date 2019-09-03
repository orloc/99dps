package common

import (
	"log"
)

func CheckErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}

func GetScreenDims(v ViewProperties, maxX, maxY int) (int, int, int, int) {
	x1 := int(v.X1 * float64(maxX))
	x2 := int(v.X2*float64(maxX)) - 1
	y1 := int(v.Y1 * float64(maxY))
	y2 := int(v.Y2*float64(maxY)) - 1

	return x1, y1, x2, y2
}
