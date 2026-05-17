SRC := ./src
BIN := ./bin

Package := ./...

_RUN_TEST = mkdir -p test-output && cd $(SRC) && go test -v -count=1 -run=$(Test) $(Package) -coverprofile=../test-output/single-$(Test).cover.out

.PHONY: all build client test vet fmt race clean test-one coverage-one

all: build client

build:
	cd $(SRC) && go build -o ../$(BIN)/runtime .

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
	rm -f $(BIN)/runtime && rm -f $(BIN)/client && cd $(SRC) && go clean . && go clean ./cmd/client && go clean -cache 

test-one:
	${_RUN_TEST}

coverage-one:
	${_RUN_TEST}
	cd $(SRC) && go run ./cmd/tools/coverage/ ../test-output/single-$(Test).cover.out . $(Package)
