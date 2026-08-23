BINARY := tyr

.PHONY: build run test vet lint fmt install clean

build:
	go build -o $(BINARY) .

run:
	go run .

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

install:
	go install .

clean:
	rm -f $(BINARY)
