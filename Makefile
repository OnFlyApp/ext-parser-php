.PHONY: all wasm native test clean

WASM_BIN := bin/php-parser.wasm
NATIVE_BIN := bin/php-parser
# Reproducible: the release job refuses to publish different bytes for a tag.
# Embed only the PHP grammar blob (default embeds all 200+ grammars, ~38 MB).
TAGS := grammar_subset grammar_subset_php
FLAGS := -tags "$(TAGS)" -trimpath -ldflags="-s -w -buildid="

all: wasm native

wasm:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm go build $(FLAGS) -o $(WASM_BIN) .

native:
	@mkdir -p bin
	CGO_ENABLED=0 go build $(FLAGS) -o $(NATIVE_BIN) .

test:
	CGO_ENABLED=0 go test -tags "$(TAGS)" ./... -count=1 -timeout 300s

clean:
	@rm -rf bin/
