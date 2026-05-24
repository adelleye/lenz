# Provider Adapters

Lenz Core keeps provider-specific behavior behind a transfer provider adapter.
The ledger, balances, transaction history, idempotency, duplicate provider-event
protection, and reconciliation state remain owned by Lenz Core.

## Interface

The current adapter contract is:

```go
type TransferProvider interface {
	Name() string
	NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error)
	InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error)
	RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error)
	ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error)
}
```

Adapters translate provider behavior into Lenz Core transfer results and
webhook events. They do not post journals, mutate balances, write transaction
history, or decide tenant scoping.

## MockNIPProvider

`MockNIPProvider` is the only implemented adapter. It is demo-only and is wired
for the mock transfer routes. It supports:

- inbound webhook parsing for successful, pending, failed, duplicate, delayed,
  and reversal scenarios;
- outbound transfer initiation for successful, pending, and failed outcomes;
- generated provider references when a request omits one;
- requery of initiated mock transfers;
- account-name enquiry simulation.

Mock scenarios are controlled by the optional `scenario` JSON/request field:
`success`, `pending`, `failed`, `duplicate`, `delayed`, or `reversal`.
Duplicate protection is still enforced by Lenz Core through provider event IDs,
not by trusting the mock provider.

## Future Adapters

A future Monnify, Interswitch, NIBSS, Providus, sponsor-bank, or BankOne
adapter should implement `TransferProvider` and be registered when constructing
`corebanking.Service`.

No fallback provider is wired into the prototype. That is intentional: the
current provider boundary is demo scaffolding, not the production CBA provider
architecture. A real NIBSS/NIP or sponsor-bank adapter should be designed as a
production slice with tenant credentials, signed webhooks, requery, and
reconciliation behavior, not hidden behind the mock routes.

The adapter should:

- load credentials from runtime configuration or a secrets manager, never from
  checked-in code;
- map provider status codes into `pending`, `succeeded`, or `failed`;
- generate or return provider references and provider event IDs exactly as the
  provider defines them;
- verify webhook authenticity before returning a `ProviderWebhookEvent`;
- return parsed provider payloads to Lenz Core without posting ledger entries.

## Core Ownership

These concerns must remain in Lenz Core, not in provider adapters:

- ledger journal creation and balanced debit/credit postings;
- ledger-backed available and ledger balances;
- transaction history;
- institution and tenant scoping;
- idempotency-key enforcement;
- duplicate provider-event protection;
- insufficient-funds handling;
- reversal recording;
- reconciliation state and auditability.

Demo-only behavior must stay in mock provider or demo route paths. Production
style endpoints must receive an explicit institution scope instead of silently
falling back to demo institution IDs.
