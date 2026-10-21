# ext-parser-php

PHP 8.x parser plugin for **Onfly**. Built on [gotreesitter](https://github.com/odvcencio/gotreesitter) (pure-Go tree-sitter, no CGO) with the tree-sitter-php grammar. Compiles to a WASI module for Onfly's wazero sandbox, or a native binary for stdio use.

```bash
onfly ext install php
```

Handles `.php`, `.phtml` and `.inc`. `-rc` tags are GitHub pre-releases and are not registered; install one by hand with `onfly ext install ./php-parser.wasm --extensions=".php,.phtml,.inc"`.

## Chunks

Input is one file on stdin, argv[1] is its path, output is the standard `[]Chunk` JSON.

| `type` | Source node | Notes |
|---|---|---|
| `class` `interface` `trait` `enum` | `*_declaration` | header only (docblock, attributes, signature); members are separate chunks |
| `enum_case` `method` `property` `constant` `trait_use` | class body members | `receiver` = owning type |
| `function` | `function_definition` | |
| `closure` | top-level statement holding a closure / arrow fn | |
| `anonymous_class` | `new class {}` | `receiver` = enclosing function or method |
| `statement` | other top-level code | consecutive statements merged |
| `file` | whole file | fallback when nothing else matched |

Metadata: `name`, `fqn`, `namespace`, `receiver`, `start_line`, `end_line`, and where they apply `signature`, `params`, `return_type`, `visibility`, `modifiers`, `attributes`, `extends`, `implements`, `backing_type`, `value`, `type`, `part`, and the type names below.

`namespace` and `use` statements are not chunks. The parser reads them to qualify type names, and the host reads the `use` lines from the source to resolve calls.

### Type names

Names are fully qualified, through the file's `use` imports and namespace; scalar and pseudo types (`int`, `string`, `self`, `mixed`, ...) are left out.

| Key | On | Value |
|---|---|---|
| `param_types` | `method`, `function` | the classes the parameters are typed with, in order, comma separated, no repeats. A promoted constructor parameter counts. |
| `type_fqn` | `property` | the classes of the declared `type` (`?Foo`, `Foo\|Bar`), comma separated |
| `handles` | `method` | the one class a handler or listener entry handles: the method is `__invoke`, `handle`, `execute`, `handle*` or `on*`, and its first parameter is typed with exactly one class (`handle(SomeCommand $c)`, `__invoke(SomeEvent $e)`). A union type gives nothing. |

The host uses `handles` to connect the code that builds `SomeCommand` to the handler, since a bus, not a call, joins them.

Docblocks and `#[Attributes]` are part of the chunk. Bodies over 8 KB are split at line boundaries (`part=1/3`). Files over 80 KiB are not parsed: they come back as plain `file` chunks (`reason=too_large`), and files over 2 MiB yield `[]`. The host cancels a parse at 5 s (and then keeps the stale chunks), while parsing costs about 11 ms/KB on a laptop and about 33 ms/KB on a CI-class machine. Declarations inside `if`/`try` blocks (`if (!function_exists(...)) { function ... }`) are chunked like top-level ones. Syntax errors do not fail the file: what parsed is chunked and a warning goes to stderr.

## Known limitations

- The bundled tree-sitter-php grammar predates two PHP 8.5 constructs: clone-with (`clone($o, ['a' => 1])`) and `final` promoted properties. Each yields a local parse error and a stderr warning; every declaration is still chunked.
- The wasm is about 18 MB (tree-sitter runtime plus the PHP grammar). Expect about 4.7 s to compile it once per host start and about 90 ms per file, which is the grammar load. An 80 KiB file takes about 1 s on a laptop.
- `.blade.php` is not handled here; use a separate Blade plugin.

## Build

```bash
make wasm     # bin/php-parser.wasm (reproducible, Go 1.27+)
make native   # bin/php-parser
make test
```

## Test

```bash
make test                                                    # unit and golden tests
go test -tags "grammar_subset grammar_subset_php" -run Golden -update    # regenerate testdata/*.golden.json after an intended change
make wasm && PHP_WASM=bin/php-parser.wasm go test -tags "grammar_subset grammar_subset_php" -run TestWasm -v .   # built module under wazero
```

The golden files in `testdata/` pin content and metadata of every chunk. The wazero test checks the chunk types, the 25 MiB size budget, and that the largest file we still parse stays under the host's 5 s limit. CI also runs a short fuzz pass and builds the wasm twice to check the bytes match.

The chunk contract is described in the [`cli` docs](https://github.com/OnFlyApp/cli/blob/main/docs/parsers.md). Plugins are listed in the [registry](https://github.com/OnFlyApp/registry).

## Use

```yaml
indexer:
  include: ["**/*.php"]
  parsers:
    - extensions: [".php", ".phtml", ".inc"]
      type: "wasm"
      path: ".onfly/plugins/php-parser.wasm"
```

The router prefers the longest suffix, so a separate `.blade.php` plugin can coexist with this one.

## Author

[Oleksii Ivanov](https://github.com/axitdev)

## License

Licensed under the **Functional Source License, Version 1.1, ALv2 Future License** (SPDX: `FSL-1.1-ALv2`), see [LICENSE](LICENSE).

Free for any purpose other than a competing use: making the software available in a commercial product or service that substitutes for it. Each version converts to Apache 2.0 two years after it is released.

Third-party licenses of the components embedded in the wasm: see THIRD_PARTY_NOTICES.
