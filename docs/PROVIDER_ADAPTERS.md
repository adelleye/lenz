# Provider Adapters

Provider adapters translate external provider behavior into a small Lenz Core
contract. The ledger and reconciliation decisions stay inside Lenz Core.

## Interface

The current transfer-provider contract is:

```go
type TransferProvider interface {
	Name() string
	NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error)
	InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error)
	RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error)
	ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error)
}
```

Adapters may parse provider payloads, map provider statuses, and return
provider references. They must not:

- post journals;
- mutate balances;
- create or release holds;
- decide tenant scope;
- suppress reconciliation exceptions;
- silently fall back to another provider.

## Current Adapter

`MockNIPProvider` is the only implemented adapter. It is mock-only and exists to
prove provider-shaped flows locally.

It supports:

- account-name enquiry simulation;
- outbound initiation scenarios: `success`, `pending`, `failed`, `timeout`,
  and `provider_unknown`;
- inbound event parsing for success, pending, failed, duplicate, delayed, and
  reversal-style mock scenarios;
- requery of initiated mock transfers;
- explicit mock requery override scenarios: `success`, `pending`, `failed`,
  `timeout`, and `provider_unknown`;
- generated provider references when a request omits one.

Duplicate protection is enforced by Lenz Core using idempotency keys and
provider event IDs. It is not trusted to provider memory.

## Who Owns What

Provider adapter owns:

- external provider naming;
- provider reference and event parsing;
- provider status mapping;
- mock scenario behavior for local proof.

Lenz Core owns:

- institution and tenant scoping;
- customer/account policy;
- idempotency;
- duplicate provider-event protection;
- holds;
- ledger journals and postings;
- account balance cache updates;
- transaction history;
- audit events;
- reconciliation/manual-review state.

## Future Real Adapters

A future Monnify, Interswitch, NIBSS, Providus, BankOne, sponsor-bank, or other
adapter should implement the same contract, but only as an explicit production
slice.

That future work must include credentials/secrets handling, signed webhook
verification, provider-specific status mapping, idempotency/reference rules,
requery behavior, reconciliation behavior, tests, and operational docs.
