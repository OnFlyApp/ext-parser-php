<html>
<?php
declare(strict_types=1);
namespace App\Demo;

use Foo\Bar;

/** Status doc */
#[Attr]
enum Status: string implements HasLabel {
    case Active = 'a';
    public function label(): string { return $this->name; }
}

/** Doc */
final class Svc extends Base implements A, B {
    use Loggable;
    public const X = 1;
    public private(set) int $n = 0;
    public function run(): void { $f = new class { public function x() {} }; }
}

interface I { public function f(); }
trait T { }
const TOP = 3;
function helper(int $a): int { return $a |> abs(...); }
$route = fn($x) => $x;
define('A', 1);
#[\NoDiscard]
function nd(): int { return 1; }
$c = clone($o, ['a' => 1]);
