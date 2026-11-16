.PHONY: all build build-linux run test clean quy test-msg deploy

all: build

build:
	@mkdir -p bin
	go build -o bin/fundbot ./cmd/fundbot

build-linux:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/fundbot-linux-amd64 ./cmd/fundbot

deploy: build-linux
	@./deploy/setup.sh

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
