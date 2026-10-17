<?php
namespace App;

class P {
    public string $name { get => strtoupper($this->name); set { $this->name = $value; } }
    public function __construct(private readonly string $s = '') {}
    #[\Override]
    public int $x = 1;
    #[\Deprecated]
    const OLD = 1;
}
final class Q { public const F = static fn($x) => $x * 2; }
function piped($a) { return $a |> trim(...) |> strlen(...); }
