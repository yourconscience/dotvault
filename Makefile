.PHONY: build test lint clean install

build:
	go build -o dotvault ./cmd/dotvault/

test:
	go test ./cmd/dotvault/ -count=1

lint:
	shellcheck -x --severity=warning hooks/*.sh

clean:
	rm -f dotvault

install: build
	install -m 0755 dotvault $(GOPATH)/bin/dotvault 2>/dev/null || install -m 0755 dotvault $(HOME)/go/bin/dotvault
