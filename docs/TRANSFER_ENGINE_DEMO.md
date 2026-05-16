# Transfer Engine Demo

This demo proves the first Lenz Core transaction spine:

institution -> customer -> account -> ledger -> transfer-in -> transfer-out -> transaction history.

## Prerequisites

- Go 1.25.6+
- Docker with Compose

The API defaults to:

```sh
DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:5432/lenzcore?sslmode=disable'
PORT=3001
```

## Start Locally

```sh
docker compose -f infra/docker/docker-compose.yml up -d postgres redis
go run ./apps/core/cmd/migrate
go run ./apps/core
```

In another terminal:

```sh
curl -s http://localhost:3001/api/v1/health
```

Expected:

```json
{"status":"ok"}
```

## Seed Demo Data

```sh
curl -s -X POST http://localhost:3001/api/v1/demo/seed
```

Important seeded IDs:

```text
institution_id=11111111-1111-1111-1111-111111111111
customer_id=33333333-3333-3333-3333-333333333333
account_id=44444444-4444-4444-4444-444444444444
clearing_account_id=55555555-5555-5555-5555-555555555555
```

## Read Customer Accounts

```sh
curl -s http://localhost:3001/api/v1/customers/33333333-3333-3333-3333-333333333333/accounts
```

Expected: one customer wallet account with account number `9990000001`.

## Transfer In

```sh
curl -s -X POST http://localhost:3001/api/v1/transfers/mock/inbound \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-in-001' \
  -d '{
    "account_id": "44444444-4444-4444-4444-444444444444",
    "amount_minor": 500000,
    "provider_event_id": "mock-event-in-001",
    "provider_reference": "nip-in-001",
    "narration": "Mock inbound transfer"
  }'
```

Expected: `status` is `succeeded`, `journal_entry_id` is present, and the
journal contains one debit and one credit for the same minor-unit amount.

Check balance:

```sh
curl -s http://localhost:3001/api/v1/accounts/44444444-4444-4444-4444-444444444444/balance
```

Expected: `available_minor` and `ledger_minor` are `500000`.

## Duplicate Protection

Repeat the transfer-in call with the same `Idempotency-Key`; the returned
transfer ID should be unchanged and the balance should remain `500000`.

Repeat it with a new `Idempotency-Key` but the same `provider_event_id`; the
returned transfer ID should still be the original transfer and the balance
should remain `500000`.

## Transfer Out

```sh
curl -s -X POST http://localhost:3001/api/v1/transfers/mock/outbound \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-out-001' \
  -d '{
    "account_id": "44444444-4444-4444-4444-444444444444",
    "amount_minor": 125000,
    "provider_reference": "nip-out-001",
    "narration": "Mock outbound transfer"
  }'
```

Expected: `status` is `succeeded`.

Balance should now be `375000`.

## Pending And Failed Transfers

Pending transfer, no ledger posting:

```sh
curl -s -X POST http://localhost:3001/api/v1/transfers/mock/inbound \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-pending-001' \
  -d '{
    "account_id": "44444444-4444-4444-4444-444444444444",
    "amount_minor": 100000,
    "provider_event_id": "mock-event-pending-001",
    "status": "pending",
    "narration": "Pending inbound transfer"
  }'
```

Failed transfer, no ledger posting:

```sh
curl -s -X POST http://localhost:3001/api/v1/transfers/mock/outbound \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-failed-001' \
  -d '{
    "account_id": "44444444-4444-4444-4444-444444444444",
    "amount_minor": 999999999,
    "narration": "Insufficient funds demo"
  }'
```

Expected: `status` is `failed`, `failure_reason` is `insufficient_funds`, and
balance is unchanged.

## Transaction History

```sh
curl -s http://localhost:3001/api/v1/accounts/44444444-4444-4444-4444-444444444444/transactions
```

Expected: succeeded rows have `journal_entry_id` and signed amounts from Lenz
postings. Pending and failed rows appear with `signed_minor: 0`.

## Journal Inspection

Use a `journal_entry_id` from a succeeded transfer:

```sh
curl -s http://localhost:3001/api/v1/admin/ledger/journal/<journal_entry_id>
```

Expected:

```json
{
  "balanced": true,
  "postings": [
    {"direction": "debit", "amount_minor": 500000},
    {"direction": "credit", "amount_minor": 500000}
  ]
}
```

## Reversal

Reverse a succeeded transfer:

```sh
curl -s -X POST http://localhost:3001/api/v1/transfers/<transfer_id>/reverse \
  -H 'Idempotency-Key: demo-reversal-001'
```

Expected: a new transfer is created with `direction: reversal`,
`reversal_of_transfer_id` set to the original transfer, and a new balanced
journal entry. The original ledger history is not deleted.

## Admin Transfer List

```sh
curl -s http://localhost:3001/api/v1/admin/transfers
```

Expected: all demo transfer records for the demo institution.
