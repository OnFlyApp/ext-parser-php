package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func kinds(t *testing.T, src string) map[string]Chunk {
	t.Helper()
	chunks, err := parse("src/x.php", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Chunk{}
	for _, c := range chunks {
		key := c.Type + " " + c.Metadata["fqn"]
		if c.Metadata["fqn"] == "" {
			key = c.Type + " " + c.Metadata["name"]
		}
		out[key] = c
	}
	return out
}

func TestAllChunkTypes(t *testing.T) {
	src, err := os.ReadFile("testdata/all.php")
	if err != nil {
		t.Fatal(err)
	}
	got := kinds(t, string(src))
	for _, want := range []string{
		`namespace App\Demo`, `use use Foo\Bar;`,
		`enum App\Demo\Status`, `enum_case App\Demo\Status::Active`, `method App\Demo\Status::label`,
		`class App\Demo\Svc`, `trait_use Loggable`, `constant App\Demo\Svc::X`, `property App\Demo\Svc::$n`,
		`method App\Demo\Svc::run`, `anonymous_class class@anonymous`,
		`interface App\Demo\I`, `trait App\Demo\T`, `constant App\Demo\TOP`,
		`function App\Demo\helper`, `function App\Demo\nd`, `closure closure`, `statement statements`,
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing chunk %q; have %v", want, keys(got))
		}
	}
	if c := got[`class App\Demo\Svc`]; strings.Contains(c.Content, "run()") || c.Metadata["extends"] != "Base" || c.Metadata["implements"] != "A, B" {
		t.Errorf("class header chunk wrong: %+v", c)
	}
	if c := got[`enum App\Demo\Status`]; !strings.HasPrefix(c.Content, "/** Status doc */") || c.Metadata["backing_type"] != "string" {
		t.Errorf("enum chunk wrong: %+v", c)
	}
	if c := got[`method App\Demo\Svc::run`]; c.Metadata["receiver"] != `App\Demo\Svc` || c.Metadata["visibility"] != "public" || c.Metadata["return_type"] != "void" {
		t.Errorf("method chunk wrong: %+v", c)
	}
}

func TestEmptyAndTemplate(t *testing.T) {
	if chunks, _ := parse("a.php", nil); chunks == nil || len(chunks) != 0 {
		t.Fatalf("empty file must be [], got %#v", chunks)
	}
	got := kinds(t, "<h1>hello</h1>\n")
	if _, ok := got["file src/x.php"]; len(got) != 1 || !ok {
		t.Fatalf("template must yield one file chunk: %v", keys(got))
	}
}

func TestSyntaxErrorKeepsParsedDeclarations(t *testing.T) {
	got := kinds(t, "<?php\nfunction ok() {}\nclass { \n")
	if _, ok := got["function ok"]; !ok {
		t.Fatalf("lost valid function: %v", keys(got))
	}
}

func TestLongFunctionIsSplit(t *testing.T) {
	body := strings.Repeat("    $x = 1;\n", 2000)
	chunks, err := parse("a.php", []byte("<?php\nfunction big() {\n"+body+"}\n"))
	if err != nil || len(chunks) < 2 || chunks[0].Metadata["part"] == "" {
		t.Fatalf("expected split parts, got %d chunks, err %v", len(chunks), err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("<?php class A { function f() {} }"))
	f.Fuzz(func(t *testing.T, b []byte) {
		// Syntax garbage is fine; a recovered panic is not.
		if _, err := parse("f.php", b); err != nil && strings.Contains(err.Error(), "panic") {
			t.Fatal(err)
		}
	})
}

func keys(m map[string]Chunk) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}

func TestPHP85(t *testing.T) {
	src, err := os.ReadFile("testdata/php85.php")
	if err != nil {
		t.Fatal(err)
	}
	got := kinds(t, string(src))
	for _, want := range []string{
		`property App\P::$name`, `property App\P::$x`, `constant App\P::OLD`,
		`constant App\Q::F`, `function App\piped`, `method App\P::__construct`,
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q; have %v", want, keys(got))
		}
	}
	if got[`property App\P::$x`].Metadata["attributes"] != `#[\Override]` || got[`constant App\P::OLD`].Metadata["attributes"] != `#[\Deprecated]` {
		t.Errorf("attributes not captured")
	}
}

// Known grammar gaps (tree-sitter-php lags PHP 8.5): clone-with and `final`
// promoted properties give a local ERROR node. Declarations must still chunk.
func TestKnownGrammarGapsDoNotLoseDeclarations(t *testing.T) {
	got := kinds(t, "<?php\nclass A { public function __construct(public final int $id) {} }\nfunction f() { return clone($o, ['a' => 1]); }\n")
	for _, want := range []string{"class A", "method A::__construct", "function f"} {
		if _, ok := got[want]; !ok {
			t.Errorf("lost %q; have %v", want, keys(got))
		}
	}
}

func TestConditionalDeclarations(t *testing.T) {
	src, err := os.ReadFile("testdata/conditional.php")
	if err != nil {
		t.Fatal(err)
	}
	got := kinds(t, string(src))
	for _, want := range []string{"function str_starts", "class Legacy", "method Legacy::run", "class Modern", "interface Maybe"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q; have %v", want, keys(got))
		}
	}
	if c := got["function str_starts"]; !strings.HasPrefix(c.Content, "/** Polyfill. */") {
		t.Errorf("docblock lost: %q", c.Content)
	}
}

func TestNestedAnonymousClasses(t *testing.T) {
	chunks, _ := parse("a.php", []byte("<?php class H { function m() { return new class { function i() { return new class {}; } }; } }"))
	n := 0
	for _, c := range chunks {
		if c.Type == "anonymous_class" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("want 2 anonymous classes, got %d", n)
	}
}

func TestOversizedFileBecomesPlainText(t *testing.T) {
	src := "<?php\n" + strings.Repeat("// padding padding padding\n", maxParseSize/20)
	chunks, err := parse("big.php", []byte(src))
	if err != nil || len(chunks) < 2 {
		t.Fatalf("want split file chunks, got %d, err %v", len(chunks), err)
	}
	for _, c := range chunks {
		if c.Type != "file" || c.Metadata["reason"] != "too_large" {
			t.Fatalf("unexpected chunk %s %v", c.Type, c.Metadata)
		}
	}
}

func TestHugeFileIsSkipped(t *testing.T) {
	chunks, err := parse("huge.php", []byte("<?php\n"+strings.Repeat("x", maxFallbackSize)))
	if err != nil || chunks == nil || len(chunks) != 0 {
		t.Fatalf("want [], got %d chunks, err %v", len(chunks), err)
	}
}

func TestSplitKeepsValidUTF8(t *testing.T) {
	body := strings.Repeat("$s = 'ёёёёёёёёёёёёёёёёёёёё'; ", 600) // one long line: forces the no-newline fallback
	chunks, err := parse("a.php", []byte("<?php\nfunction u() { "+body+"}\n"))
	if err != nil || len(chunks) < 2 {
		t.Fatalf("expected split, got %d, err %v", len(chunks), err)
	}
	for _, c := range chunks {
		if !utf8.ValidString(c.Content) {
			t.Fatalf("invalid utf-8 in part %s", c.Metadata["part"])
		}
	}
}

// Golden files pin content and metadata, not just chunk presence. Regenerate
// with: go test -tags "grammar_subset grammar_subset_php" -run Golden -update
var update = flag.Bool("update", false, "rewrite golden files")

func TestGolden(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.php")
	phtml, _ := filepath.Glob("testdata/*.phtml")
	files = append(files, phtml...)
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		chunks, err := parse("src/"+filepath.Base(f), src)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.MarshalIndent(chunks, "", "  ")
		golden := strings.TrimSuffix(f, filepath.Ext(f)) + ".golden.json"
		if *update {
			if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run with -update)", err)
		}
		if strings.TrimSpace(string(want)) != string(got) {
			t.Errorf("%s differs from %s (run with -update if intended)", f, golden)
		}
	}
}
