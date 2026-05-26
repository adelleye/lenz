# Documentation Map

Start here if the repository feels large.

## Current Guides

- [README](../README.md): what the project does, how to run it, and how to
  prove it works.
- [Project Structure](../PROJECT_STRUCTURE.md): folder map, request path, and
  main API areas.
- [Architecture Status](ARCHITECTURE_STATUS.md): how the current system hangs
  together.
- [Architecture Decisions](ARCHITECTURE_DECISIONS.md): money and provider
  rules that should not be casually changed.
- [Provider Adapters](PROVIDER_ADAPTERS.md): what belongs in provider adapters
  versus the Lenz ledger/service layer.
- [Test Plan](TEST_PLAN.md): durable verification checklist.
- [Test Results](TEST_RESULTS.md): latest known proof run and residual gaps.

## Product And Risk Notes

- [Product Requirements Document](../Lenz-Core_PRD.md): product north star, not
  the current implementation guide.
- [Simple Transaction CBA v0.1](SIMPLE_TRANSACTION_CBA_V0_1.md): product scope
  and build sequence.
- [Limits And Risk Placeholders](LIMITS_AND_RISK_PLACEHOLDERS.md): controls
  intentionally not implemented yet.

## Historical Reference

- `docs/cba-goals/`: goal briefs used to build recent slices.
- `docs/testing/`: early scenario checklists for the first CBA flows.
- [Goal Progress](GOAL_PROGRESS.md): short historical build log.
- [Transfer Engine Demo](TRANSFER_ENGINE_DEMO.md): notes for the fuller
  mock-provider demo script.

The current source of truth is the code, OpenAPI spec, migrations, README, and
the proof scripts. Older goal documents explain why features were added, but
they are not the fastest way to learn how to run the app today.
