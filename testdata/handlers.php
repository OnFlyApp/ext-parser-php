<?php

declare(strict_types=1);

namespace App\Billing\Application;

use App\Billing\Domain\CancelOrder;
use App\Billing\Domain\{Refund, Credit as CreditNote};
use function App\helper;

final class CancelOrderHandler
{
    private Logger $logger;

    public function __construct(private Repo $repo, private ?Calculator $calc = null, int $retries = 3) {}

    public function __invoke(CancelOrder $command): void {}

    public function handle(Refund|CreditNote $either): void {}

    public function onRefunded(\Vendor\Events\Refunded $event, int $n): void {}

    public function handleCredit(CreditNote $credit): void {}

    public function render(string $s, Other $o): void {}

    public function execute(): void {}
}
