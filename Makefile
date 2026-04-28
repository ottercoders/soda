BINARY := soda
PKG    := ./cmd/soda

.PHONY: all build test tidy clean install run

all: build

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./...

tidy:
	go mod tidy

run: build
	./$(BINARY)

install:
	go install $(PKG)

clean:
	rm -f $(BINARY)
