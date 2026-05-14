BINARY=chawrtd

.PHONY: build run fmt clean

build:
	go build -o bin/$(BINARY) ./cmd/chawrtd

run:
	go run ./cmd/chawrtd

fmt:
	gofmt -w ./cmd ./internal

clean:
	rm -rf bin/
