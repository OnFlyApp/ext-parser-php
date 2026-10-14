# ext-parser-php

PHP 8.x parser plugin for **Onfly**. Built on [gotreesitter](https://github.com/odvcencio/gotreesitter) (pure-Go tree-sitter, no CGO) with the tree-sitter-php grammar. Compiles to a WASI module for Onfly's wazero sandbox, or a native binary for stdio use.

> **Status: release candidate.** `-rc` tags are published as GitHub pre-releases and are **not** added to the registry. Install an rc by hand:
>
> ```bash
> curl -L -o php-parser.wasm https://github.com/OnFlyApp/ext-parser-php/releases/download/v0.1.0-rc.1/php-parser.wasm
> onfly ext install ./php-parser.wasm --extensions=".php,.phtml,.inc"
> ```
>
> Stable `vX.Y.Z` tags are registered, and then `onfly ext install php` works.

## Chunks

Input is one file on stdin, argv[1] is its path, output is the standard `[]Chunk` JSON.

| `type` | Source node | Notes |
|---|---|---|
| `namespace` | `namespace_definition` | header only |
| `use` | `namespace_use_declaration` | |
| `class` `interface` `trait` `enum` | `*_declaration` | header only (docblock, attributes, signature); members are separate chunks |
| `enum_case` `method` `property` `constant` `trait_use` | class body members | `receiver` = owning type |
| `function` | `function_definition` | |
| `closure` | top-level statement holding a closure / arrow fn | |
| `anonymous_class` | `new class {}` | `receiver` = enclosing function or method |
| `statement` | other top-level code | consecutive statements merged |
| `file` | whole file | fallback when nothing else matched |

Metadata: `name`, `fqn`, `namespace`, `receiver`, `start_line`, `end_line`, and where they apply `signature`, `params`, `return_type`, `visibility`, `modifiers`, `attributes`, `extends`, `implements`, `backing_type`, `value`, `type`, `part`.

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

Third-party licenses: see THIRD_PARTY_NOTICES.
