# CBA v0.1 Build 1: Customers Verification

Date: 2026-05-25
Branch: `goal/cba-v0.1-01-customers`

## Scope Checked

This verifies `docs/cba-goals/01-customers.md`.

Implemented endpoints:

- `POST /api/v1/customers`
- `GET /api/v1/customers/{customer_id}`

Intentional non-scope:

- No account creation.
- No money movement changes.
- No BVN/NIN provider lookup.
- No watch-list integration.
- No frontend.
- No auth redesign.

## Requirements Pass List

| Requirement | Result | Evidence |
| --- | --- | --- |
| OpenAPI source updated | PASS | `design/openapi/core/corebanking.yaml` defines customer create/get and customer metadata fields. |
| Strict generated handler path preserved | PASS | Handwritten handler implements strict request/response methods; generated `*.gen.go` files are ignored. |
| Existing module structure followed | PASS | Changes stay in `model.go`, `repository.go`, `service.go`, `handler.go`, and focused SQL repository files. |
| Existing customers table used | PASS | No migration added; `customers.meta` stores `customer_type`, `kyc_tier`, `bvn_status`, and `nin_status`. |
| Individual customer supported | PASS | Service, handler, and Postgres tests cover individual creation. |
| Business customer supported | PASS | Service test covers `customer_type=business` with `business_name` stored in metadata. |
| Tenant scope enforced | PASS | Institution comes from auth principal; mismatched `X-Institution-ID` is rejected. |
| Branch institution checked | PASS | SQL repository requires `branch_id` to belong to the same institution. |
| Invalid input rejected | PASS | Tests cover missing individual name and invalid customer type. |
| Raw SQL errors not leaked | PASS | Handler test verifies sanitized internal error response with `request_id`. |
| DB row creation proven | PASS | Postgres integration and manual DB query both confirm row creation. |

## Required Commands

All required commands passed:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
git check-ignore -v apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
LENZ_INTEGRATION_DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' go test -count=1 -tags=integration ./apps/core/internal/corebanking -run TestSQLRepositoryCustomerCreateGetIntegration
```

## Manual HTTP And DB Proof

Manual local proof passed with Docker/Postgres on `localhost:55432`.

Create response:

```json
{"id":"60c6cbe5-6def-47da-88ba-30e758b288cf","institution_id":"11111111-1111-1111-1111-111111111111","branch_id":"22222222-2222-2222-2222-222222222222","customer_type":"individual","first_name":"Adaeze","last_name":"Okafor","email":"adaeze.http@example.com","phone":"+2348012345678","status":"active","kyc_tier":"tier1","bvn_status":"not_collected","nin_status":"not_collected"}
```

Get response:

```json
{"id":"60c6cbe5-6def-47da-88ba-30e758b288cf","institution_id":"11111111-1111-1111-1111-111111111111","branch_id":"22222222-2222-2222-2222-222222222222","customer_type":"individual","first_name":"Adaeze","last_name":"Okafor","email":"adaeze.http@example.com","phone":"+2348012345678","status":"active","kyc_tier":"tier1","bvn_status":"not_collected","nin_status":"not_collected"}
```

DB row evidence:

```text
60c6cbe5-6def-47da-88ba-30e758b288cf|11111111-1111-1111-1111-111111111111|22222222-2222-2222-2222-222222222222|individual|tier1|not_collected|not_collected
```

## Deferred Gaps

- Real BVN/NIN lookup is intentionally not implemented.
- Sensitive BVN/NIN values are intentionally not stored.
- Customer search, document upload, watch-list, maker-checker, and frontend are intentionally deferred.
