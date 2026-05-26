# Transfer Engine Demo

`scripts/demo_transfer_spine.sh` is the one-command proof for the fuller mock
provider transaction spine.

Run it from the repository root:

```sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

Expected final line:

```text
DEMO TRANSFER SPINE: PASS
```

## What The Script Does

The script:

- resets the local Docker Postgres/Redis services for a clean run;
- runs migrations;
- runs Go tests and SQL integration checks;
- starts the API with `LENZ_DEMO_MODE=true`;
- seeds a demo institution, branch, customer, wallet account, and clearing
  account;
- exercises mock inbound, outbound, pending, failed, reversal, admin, history,
  reconciliation, and requery flows over HTTP;
- verifies journals balance and cached balances reconcile to ledger postings
  and active holds.

## What It Proves

- successful inbound credits once;
- duplicate idempotency keys and duplicate provider events do not double-post;
- successful outbound debits once;
- pending outbound creates a hold without posting;
- failed pending outbound releases the hold without posting;
- successful pending outbound consumes the hold and posts one journal;
- pending/provider-unknown requery keeps unresolved transfers visible;
- reversal creates a new ledger event and can enter manual review;
- transaction history comes from Lenz records;
- admin transfer, journal, and reconciliation paths expose the expected state.

## What It Does Not Prove

The demo does not connect to real NIBSS, BankOne, Monnify, Interswitch,
Providus, or a sponsor bank. It does not prove production auth, signed
webhooks, maker-checker, KYC/BVN/NIN, limits, fraud monitoring, statements, or
regulatory reporting.

For the non-demo CBA v0.1 proof, run:

```sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
```
