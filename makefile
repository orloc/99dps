GOCMD=go
GOBUILD=${GOCMD} build
GOCLEAN=${GOCMD} clean

all: build clean

build:
	go build -o 99dps -ldflags="-s -w" .
clean:
	${GOCLEAN}
	rm -f 99dps

