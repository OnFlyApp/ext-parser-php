package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const (
	// The host cancels a parse after 5 s. Measured ~11 ms/KB under wazero
	// (+~90 ms startup): 299 KiB took 3.2 s. 200 KiB leaves headroom for slower
	// machines; bigger files are generated or vendored anyway.
	maxFileSize  = 200 << 10
	maxChunkSize = 8 << 10 // longer function bodies are split at line boundaries
)

// Chunk is the universal contract defined in the Onfly SDD.
type Chunk struct {
	FilePath string            `json:"file_path"`
	Type     string            `json:"type"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
}

// fatalError writes to stderr so the Onfly core can log it without
// breaking the stdout JSON pipeline.
func fatalError(err error) {
	fmt.Fprintf(os.Stderr, "onfly-php-parser error: %v\n", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		fatalError(fmt.Errorf("missing file path argument"))
	}
	filePath := os.Args[1]
	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatalError(fmt.Errorf("failed to read stdin: %w", err))
	}
	chunks, err := parse(filePath, src)
	if err != nil {
		fatalError(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(chunks); err != nil {
		fatalError(fmt.Errorf("failed to encode json: %w", err))
	}
}

type walker struct {
	src    []byte
	lang   *gotreesitter.Language
	file   string
	lines  []int // byte offset of each line start
	chunks []Chunk
}

func parse(filePath string, src []byte) (chunks []Chunk, err error) {
	defer func() {
		if r := recover(); r != nil {
			chunks, err = nil, fmt.Errorf("parser panic: %v", r)
		}
	}()
	// Initialized empty so JSON serializes as [] instead of null.
	chunks = []Chunk{}
	if len(src) == 0 {
		return chunks, nil
	}
	if len(src) > maxFileSize {
		fmt.Fprintf(os.Stderr, "onfly-php-parser: %s is %d bytes, skipped\n", filePath, len(src))
		return chunks, nil
	}
	lang := grammars.PhpLanguage()
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("failed to parse php file: %w", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		fmt.Fprintf(os.Stderr, "onfly-php-parser: %s has syntax errors, chunking what parsed\n", filePath)
	}
	w := &walker{src: src, lang: lang, file: filePath, lines: []int{0}, chunks: chunks}
	for i, b := range src {
		if b == '\n' {
			w.lines = append(w.lines, i+1)
		}
	}
	w.block(root, "")
	if len(w.chunks) == 0 {
		// Nothing declarable (template, empty script): keep the whole file searchable.
		w.emit("file", 0, uint32(len(src)), map[string]string{"name": filePath})
	}
	return w.chunks, nil
}

// ---- helpers ----

func (w *walker) typ(n *gotreesitter.Node) string { return n.Type(w.lang) }
func (w *walker) text(n *gotreesitter.Node) string {
	if n == nil {
		return ""
	}
	return string(w.src[n.StartByte():n.EndByte()])
}
func (w *walker) field(n *gotreesitter.Node, name string) *gotreesitter.Node {
	return n.ChildByFieldName(name, w.lang)
}

// children returns the direct children of n whose type is in types.
func (w *walker) children(n *gotreesitter.Node, types ...string) []*gotreesitter.Node {
	var out []*gotreesitter.Node
	for i := 0; i < n.ChildCount(); i++ {
		c := n.Child(i)
		t := w.typ(c)
		for _, want := range types {
			if t == want {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (w *walker) contains(n *gotreesitter.Node, types ...string) bool {
	t := w.typ(n)
	for _, want := range types {
		if t == want {
			return true
		}
	}
	for i := 0; i < n.ChildCount(); i++ {
		if w.contains(n.Child(i), types...) {
			return true
		}
	}
	return false
}

// docStart widens a node's start to include a docblock that directly precedes it.
func (w *walker) docStart(n *gotreesitter.Node) uint32 {
	p := n.PrevSibling()
	if p == nil || w.typ(p) != "comment" || !strings.HasPrefix(w.text(p), "/**") {
		return n.StartByte()
	}
	if strings.Count(string(w.src[p.EndByte():n.StartByte()]), "\n") > 1 {
		return n.StartByte()
	}
	return p.StartByte()
}

func (w *walker) line(off uint32) int {
	return sort.Search(len(w.lines), func(i int) bool { return w.lines[i] > int(off) })
}

func (w *walker) emit(typ string, start, end uint32, meta map[string]string) {
	for end > start && strings.TrimSpace(string(w.src[end-1:end])) == "" {
		end--
	}
	if end <= start {
		return
	}
	meta["start_line"] = strconv.Itoa(w.line(start))
	meta["end_line"] = strconv.Itoa(w.line(end - 1))
	content := string(w.src[start:end])
	if len(content) <= maxChunkSize || typ == "class" || typ == "interface" || typ == "trait" || typ == "enum" {
		w.chunks = append(w.chunks, Chunk{FilePath: w.file, Type: typ, Content: content, Metadata: meta})
		return
	}
	var parts []string
	for len(content) > maxChunkSize {
		cut := strings.LastIndex(content[:maxChunkSize], "\n")
		if cut <= 0 {
			cut = maxChunkSize
			for cut > 0 && !utf8.RuneStart(content[cut]) {
				cut--
			}
		}
		parts = append(parts, content[:cut])
		content = content[cut:]
	}
	parts = append(parts, content)
	for i, p := range parts {
		m := make(map[string]string, len(meta)+1)
		for k, v := range meta {
			m[k] = v
		}
		m["part"] = fmt.Sprintf("%d/%d", i+1, len(parts))
		w.chunks = append(w.chunks, Chunk{FilePath: w.file, Type: typ, Content: p, Metadata: m})
	}
}

func join(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + `\` + name
}

// ---- walk ----

var declTypes = []string{"function_definition", "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration"}

// containers hold nested statements; we only descend into them when they declare
// something (`if (!function_exists('x')) { function x() {} }`).
var containers = map[string]bool{
	"if_statement": true, "else_clause": true, "else_if_clause": true, "compound_statement": true,
	"colon_block": true, "try_statement": true, "catch_clause": true, "finally_clause": true,
}

// block walks the statements of the program, a braced namespace body, or a
// container statement that declares something.
func (w *walker) block(parent *gotreesitter.Node, ns string) {
	nested := containers[w.typ(parent)]
	var run []*gotreesitter.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		first, last := run[0], run[len(run)-1]
		w.emit("statement", w.docStart(first), last.EndByte(), map[string]string{"name": "statements", "namespace": ns})
		run = nil
	}
	for i := 0; i < parent.ChildCount(); i++ {
		c := parent.Child(i)
		switch t := w.typ(c); t {
		case "php_tag", "php_end_tag", "text", "text_interpolation", "comment", "{", "}":
		case "namespace_definition":
			flush()
			ns = w.namespace(c, ns)
		case "namespace_use_declaration":
			flush()
			w.emit("use", c.StartByte(), c.EndByte(), map[string]string{"name": strings.Join(strings.Fields(w.text(c)), " "), "namespace": ns})
		case "function_definition":
			flush()
			w.function(c, ns)
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
			flush()
			w.classLike(c, ns)
		case "const_declaration":
			flush()
			w.constant(c, ns, "")
		default:
			if containers[t] && w.contains(c, declTypes...) {
				flush()
				w.block(c, ns)
				continue
			}
			if nested && !strings.HasSuffix(t, "_statement") {
				continue // conditions, keywords, catch types
			}
			if w.contains(c, "anonymous_function", "arrow_function") && w.typ(c) == "expression_statement" {
				flush()
				m := map[string]string{"name": "closure", "namespace": ns}
				w.emit("closure", w.docStart(c), c.EndByte(), m)
				w.anonymous(c, ns)
				continue
			}
			run = append(run, c)
			w.anonymous(c, ns)
		}
	}
	flush()
}

// namespace emits the header chunk and returns the namespace that applies afterwards.
func (w *walker) namespace(n *gotreesitter.Node, outer string) string {
	name := w.text(w.field(n, "name"))
	body := w.field(n, "body")
	end := n.EndByte()
	if body != nil {
		end = body.StartByte()
	}
	w.emit("namespace", n.StartByte(), end, map[string]string{"name": name, "fqn": name})
	if body != nil {
		w.block(body, name)
		return outer
	}
	return name
}

func (w *walker) function(n *gotreesitter.Node, ns string) {
	name := w.text(w.field(n, "name"))
	m := map[string]string{"name": name, "fqn": join(ns, name), "namespace": ns}
	w.callable(n, m)
	w.emit("function", w.docStart(n), n.EndByte(), m)
	w.anonymous(n, join(ns, name))
}

// callable fills signature, params, return type and attributes shared by functions and methods.
func (w *walker) callable(n *gotreesitter.Node, m map[string]string) {
	end := n.EndByte()
	if body := w.field(n, "body"); body != nil {
		end = body.StartByte()
	}
	m["signature"] = strings.Join(strings.Fields(string(w.src[n.StartByte():end])), " ")
	if p := w.field(n, "parameters"); p != nil {
		m["params"] = w.text(p)
	}
	if r := w.field(n, "return_type"); r != nil {
		m["return_type"] = w.text(r)
	}
	w.attributes(n, m)
}

func (w *walker) attributes(n *gotreesitter.Node, m map[string]string) {
	var attrs []string
	for _, l := range w.children(n, "attribute_list") {
		attrs = append(attrs, w.text(l))
	}
	if len(attrs) > 0 {
		m["attributes"] = strings.Join(attrs, " ")
	}
}

func (w *walker) modifiers(n *gotreesitter.Node, m map[string]string) {
	var mods []string
	for _, c := range w.children(n, "visibility_modifier", "static_modifier", "abstract_modifier", "final_modifier", "readonly_modifier", "var_modifier") {
		t := w.text(c)
		if w.typ(c) == "visibility_modifier" && !strings.Contains(t, "(") {
			m["visibility"] = t
			continue
		}
		mods = append(mods, t)
	}
	if len(mods) > 0 {
		m["modifiers"] = strings.Join(mods, " ")
	}
}

func (w *walker) classLike(n *gotreesitter.Node, ns string) {
	kind := strings.TrimSuffix(w.typ(n), "_declaration")
	name := w.text(w.field(n, "name"))
	fqn := join(ns, name)
	m := map[string]string{"name": name, "fqn": fqn, "namespace": ns}
	w.attributes(n, m)
	w.modifiers(n, m)
	for _, c := range w.children(n, "base_clause") {
		m["extends"] = strings.TrimSpace(strings.TrimPrefix(w.text(c), "extends"))
	}
	for _, c := range w.children(n, "class_interface_clause") {
		m["implements"] = strings.TrimSpace(strings.TrimPrefix(w.text(c), "implements"))
	}
	if kind == "enum" {
		for _, c := range w.children(n, "primitive_type") {
			m["backing_type"] = w.text(c)
		}
	}
	body := w.field(n, "body")
	end := n.EndByte()
	if body != nil {
		end = body.StartByte()
	}
	// Header only: members are separate chunks that point back through `receiver`.
	w.emit(kind, w.docStart(n), end, m)
	if body == nil {
		return
	}
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		switch w.typ(c) {
		case "method_declaration":
			w.method(c, ns, fqn)
		case "property_declaration":
			w.property(c, ns, fqn)
		case "const_declaration":
			w.constant(c, ns, fqn)
		case "use_declaration":
			w.emit("trait_use", w.docStart(c), c.EndByte(), map[string]string{
				"name": strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimPrefix(w.text(c), "use"), ";")), " "), "receiver": fqn, "namespace": ns})
		case "enum_case":
			cn := w.text(w.field(c, "name"))
			cm := map[string]string{"name": cn, "fqn": fqn + "::" + cn, "receiver": fqn, "namespace": ns}
			if v := w.field(c, "value"); v != nil {
				cm["value"] = w.text(v)
			}
			w.attributes(c, cm)
			w.emit("enum_case", w.docStart(c), c.EndByte(), cm)
		}
	}
}

func (w *walker) method(n *gotreesitter.Node, ns, class string) {
	name := w.text(w.field(n, "name"))
	m := map[string]string{"name": name, "fqn": class + "::" + name, "receiver": class, "namespace": ns}
	w.callable(n, m)
	w.modifiers(n, m)
	w.emit("method", w.docStart(n), n.EndByte(), m)
	w.anonymous(n, class+"::"+name)
}

func (w *walker) property(n *gotreesitter.Node, ns, class string) {
	var names []string
	for _, e := range w.children(n, "property_element") {
		for _, v := range w.children(e, "variable_name") {
			names = append(names, strings.TrimPrefix(w.text(v), "$"))
			break
		}
	}
	name := strings.Join(names, ",")
	m := map[string]string{"name": name, "fqn": class + "::$" + names0(names), "receiver": class, "namespace": ns}
	if t := w.field(n, "type"); t != nil {
		m["type"] = w.text(t)
	}
	w.attributes(n, m)
	w.modifiers(n, m)
	w.emit("property", w.docStart(n), n.EndByte(), m)
	w.anonymous(n, class)
}

func names0(n []string) string {
	if len(n) == 0 {
		return ""
	}
	return n[0]
}

func (w *walker) constant(n *gotreesitter.Node, ns, class string) {
	var names []string
	for _, e := range w.children(n, "const_element") {
		for _, v := range w.children(e, "name") {
			names = append(names, w.text(v))
			break
		}
	}
	name := strings.Join(names, ",")
	m := map[string]string{"name": name, "namespace": ns}
	if class != "" {
		m["receiver"] = class
		m["fqn"] = class + "::" + names0(names)
	} else {
		m["fqn"] = join(ns, names0(names))
	}
	w.attributes(n, m)
	w.modifiers(n, m)
	w.emit("constant", w.docStart(n), n.EndByte(), m)
	if class != "" {
		w.anonymous(n, class)
	}
}

// anonymous emits a chunk for every `new class {}` found under n.
func (w *walker) anonymous(n *gotreesitter.Node, owner string) {
	if w.typ(n) == "anonymous_class" {
		w.emit("anonymous_class", n.StartByte(), n.EndByte(), map[string]string{"name": "class@anonymous", "receiver": owner})
		owner += "::class@anonymous"
	}
	for i := 0; i < n.ChildCount(); i++ {
		w.anonymous(n.Child(i), owner)
	}
}
