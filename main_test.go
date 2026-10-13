package main

import (
	"os"
	"strings"
	"testing"
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
		if _, err := parse("f.php", b); err != nil {
			t.Skip()
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
