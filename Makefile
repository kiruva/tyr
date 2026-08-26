BINARY := tyr

.PHONY: build crossbuild run test vet lint fmt install clean

build:
	go build -o $(BINARY) .

# Every platform tyr releases for. Compiles only — it catches a build-tagged
# file that does not build long before CI does.
crossbuild:
	GOOS=linux   GOARCH=amd64 go build ./...
	GOOS=linux   GOARCH=arm64 go build ./...
	GOOS=darwin  GOARCH=arm64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...
	GOOS=windows GOARCH=amd64 go vet ./...   # vet compiles the tests too

run:
	go run .

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run
	GOOS=windows golangci-lint run   # the _windows.go half of each platform pair

fmt:
	gofmt -w .

install:
	go install .

clean:
	rm -f $(BINARY) $(BINARY).exe
