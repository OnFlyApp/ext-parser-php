package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// TestWasm runs the built module under wazero, the runtime the Onfly CLI uses.
// Enabled by PHP_WASM=bin/php-parser.wasm (CI sets it after `make wasm`).
func TestWasm(t *testing.T) {
	path := os.Getenv("PHP_WASM")
	if path == "" {
		t.Skip("PHP_WASM not set")
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(wasm) > 25<<20 {
		t.Fatalf("wasm is %d MiB, budget is 25 MiB", len(wasm)>>20)
	}
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	cm, err := r.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	run := func(src []byte) ([]Chunk, time.Duration) {
		var out bytes.Buffer
		cfg := wazero.NewModuleConfig().WithArgs("plugin", "src/x.php").
			WithStdin(bytes.NewReader(src)).WithStdout(&out).WithStderr(os.Stderr).WithName("")
		start := time.Now()
		if _, err := r.InstantiateModule(ctx, cm, cfg); err != nil {
			t.Fatal(err)
		}
		d := time.Since(start)
		var chunks []Chunk
		if err := json.Unmarshal(out.Bytes(), &chunks); err != nil {
			t.Fatal(err)
		}
		return chunks, d
	}

	src, _ := os.ReadFile("testdata/all.php")
	chunks, _ := run(src)
	types := map[string]bool{}
	for _, c := range chunks {
		types[c.Type] = true
	}
	for _, want := range []string{"namespace", "use", "enum", "enum_case", "class", "interface", "trait", "trait_use",
		"method", "property", "constant", "function", "closure", "statement", "anonymous_class"} {
		if !types[want] {
			t.Errorf("wasm output has no %q chunk", want)
		}
	}

	// The host cancels a parse at 5 s. The largest file we still parse must come
	// back well before that.
	var b strings.Builder
	b.WriteString("<?php\nnamespace Big;\n")
	for i := 0; b.Len() < maxParseSize-1024; i++ {
		fmt.Fprintf(&b, "class C%d {\n    public function a(int $x): int { return $x + %d; }\n    public function b() { foreach ([1,2,3] as $v) { echo $v; } }\n}\n", i, i)
	}
	chunks, d := run([]byte(b.String()))
	t.Logf("%d KiB -> %d chunks in %s", b.Len()>>10, len(chunks), d.Round(time.Millisecond))
	if len(chunks) == 0 {
		t.Fatal("largest accepted file produced no chunks")
	}
	if d > 4500*time.Millisecond {
		t.Fatalf("took %s; host limit is 5s, lower maxParseSize", d)
	}
}
