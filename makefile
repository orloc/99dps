GOCMD=go
GOBUILD=${GOCMD} build
GOCLEAN=${GOCMD} clean
MAIN_FILE=99dps.go

all: build clean

build:
	go build -o 99dps -ldflags="-s -w" ${MAIN_FILE}
clean:
	${GOCLEAN}
	rm -f 99dps

