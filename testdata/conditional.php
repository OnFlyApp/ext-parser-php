<?php
if (!function_exists('str_starts')) {
    /** Polyfill. */
    function str_starts(string $h, string $n): bool { return strncmp($h, $n, strlen($n)) === 0; }
}

if (PHP_VERSION_ID < 80000) {
    class Legacy { public function run() {} }
} else {
    class Modern { public function run() {} }
}

try {
    interface Maybe {}
} catch (\Throwable $e) {
    echo 'nope';
}

class Holder {
    public $anon;
    public function make() {
        return new class {
            public function inner() { return new class {}; }
        };
    }
}
