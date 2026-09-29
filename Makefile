.PHONY: all build run test clean quy test-msg

all: build

build:
	@mkdir -p bin
	go build -o bin/fundbot ./cmd/fundbot

run:
	go run ./cmd/fundbot

test:
	go test -v ./...

quy:
	go run ./cmd/fundbot -quy

test-msg:
	go run ./cmd/fundbot -test-msg "🔔 Test message from Mezon Fund Feed Bot"

clean:
	rm -rf bin
