#!/bin/bash
# Dev run: build and launch the meter. Args pass through, e.g.
#   ./run.sh -logdir /path/to/EQ/Logs -tts
go run ./cmd/99dps "$@"
