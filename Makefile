SRC := ./src
BIN := ./bin

.PHONY: all build client test vet fmt race clean

all: build client

build:
	cd $(SRC) && go build -o ../$(BIN)/harness .

client:
	cd $(SRC) && go build -o ../$(BIN)/client ./cmd/client

test:
	cd $(SRC) && go test ./...

vet:
	cd $(SRC) && go vet ./...

fmt:
	@UNFORMATTED=$$(cd $(SRC) && gofmt -l .); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "Unformatted files:"; \
		echo "$$UNFORMATTED" | sed 's/^/  /'; \
		exit 1; \
	fi

race:
	cd $(SRC) && go test -race ./...

clean:
	rm -f $(BIN)/harness && rm -f $(BIN)/client && cd $(SRC) && go clean . && go clean ./cmd/client && go clean -cache 
