package corebanking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTransactionHistoryEmptyListAndTenantScope(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "Empty", "History", "empty.history@example.com", uniqueAccountNumber("70"))

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if history == nil || len(history) != 0 {
		t.Fatalf("expected empty non-nil history, got %#v", history)
	}
	if _, err := svc.GetTransactions(ctx, "99999999-9999-9999-9999-999999999999", account.ID, ListTransactionsOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant history lookup to return not found, got %v", err)
	}
}

func TestTransactionHistoryDirectionsAndReferences(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	destination := createMemoryCustomerAccount(t, svc, ctx, "History", "Receiver", "history.receiver@example.com", uniqueAccountNumber("71"))

	credit := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "history-credit",
		Reference:      "history-credit-ref",
	})
	debit := mustInternalDebit(t, svc, ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    12000,
		CurrencyID:     "NGN",
		IdempotencyKey: "history-debit",
		Reference:      "history-debit-ref",
	})
	transfer := mustInternalTransfer(t, svc, ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destination.ID,
		AmountMinor:          7000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "history-transfer",
		Reference:            "history-transfer-ref",
	})

	sourceHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertHistoryRow(t, sourceHistory, credit.ID, TransactionDirectionCredit, 50000, credit.JournalEntryID, ProviderLedgerInternal, "history-credit-ref", nil)
	assertHistoryRow(t, sourceHistory, debit.ID, TransactionDirectionDebit, -12000, debit.JournalEntryID, ProviderLedgerInternal, "history-debit-ref", nil)
	assertHistoryRow(t, sourceHistory, transfer.ID, TransactionDirectionDebit, -7000, transfer.JournalEntryID, ProviderLedgerInternal, "history-transfer-ref", &destination.ID)

	destinationHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, destination.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sourceAccountID := DemoCustomerAccountID
	assertHistoryRow(t, destinationHistory, transfer.ID, TransactionDirectionCredit, 7000, transfer.JournalEntryID, ProviderLedgerInternal, "history-transfer-ref", &sourceAccountID)
}

func TestTransactionHistoryPaginationUsesStableTransferCursor(t *testing.T) {
	ctx, svc, store := newTestService(t)
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		transfer := mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    int64(1000 + i),
			CurrencyID:     "NGN",
			IdempotencyKey: "history-page-" + string(rune('a'+i)),
		})
		setTransferCreatedAt(t, store, transfer.ID, base)
	}

	first, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("expected first page of two rows, got %+v", first)
	}
	second, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{
		Limit:            2,
		BeforeCreatedAt:  &first[len(first)-1].CreatedAt,
		BeforeTransferID: first[len(first)-1].TransferID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("expected second page of two rows, got %+v", second)
	}
	seen := map[string]bool{}
	for _, row := range append(first, second...) {
		if seen[row.TransferID] {
			t.Fatalf("pagination returned duplicate transfer %s", row.TransferID)
		}
		seen[row.TransferID] = true
	}
}

func TestTransactionHistoryRejectsInvalidCursor(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	now := time.Now().UTC()
	if _, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{BeforeTransferID: DemoCustomerAccountID}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected before_transfer_id without before_created_at to fail, got %v", err)
	}
	if _, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{BeforeCreatedAt: &now, BeforeTransferID: "not-a-uuid"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid before_transfer_id to fail, got %v", err)
	}
}

func TestTransactionHistoryPendingFailedAndReversalRows(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	posted := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       10000,
		IdempotencyKey:    "history-reversal-credit",
		ProviderEventID:   "history-reversal-credit-event",
		ProviderReference: "history-reversal-credit-ref",
	})
	pending := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       3000,
		IdempotencyKey:    "history-pending",
		ProviderEventID:   "history-pending-event",
		ProviderReference: "history-pending-ref",
		Status:            TransferStatusPending,
	})
	failed := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       3000,
		IdempotencyKey:    "history-failed",
		ProviderReference: "history-failed-ref",
		Status:            TransferStatusFailed,
	})
	reversal := reverseTransfer(t, svc, ctx, posted.ID, "history-reversal")

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertHistoryRow(t, history, posted.ID, TransactionDirectionCredit, 10000, posted.JournalEntryID, ProviderMockNIP, "history-reversal-credit-ref", nil)
	assertHistoryRow(t, history, reversal.ID, TransactionDirectionDebit, -10000, reversal.JournalEntryID, ProviderMockNIP, "reversal:history-reversal-credit-ref", nil)
	assertHistoryRow(t, history, pending.ID, TransactionDirectionCredit, 0, nil, ProviderMockNIP, "history-pending-ref", nil)
	assertHistoryRow(t, history, failed.ID, TransactionDirectionDebit, 0, nil, ProviderMockNIP, "history-failed-ref", nil)
}

func assertHistoryRow(t *testing.T, history []Transaction, transferID, direction string, signedAmount int64, journalEntryID *string, provider, providerReference string, counterpartyAccountID *string) {
	t.Helper()
	for _, row := range history {
		if row.TransferID != transferID {
			continue
		}
		if row.Direction != direction || row.SignedAmountMinor != signedAmount || row.Provider != provider || row.ProviderReference != providerReference {
			t.Fatalf("history row mismatch for transfer %s: %+v", transferID, row)
		}
		if (journalEntryID == nil) != (row.JournalEntryID == nil) {
			t.Fatalf("history journal mismatch for transfer %s: %+v", transferID, row)
		}
		if journalEntryID != nil && *row.JournalEntryID != *journalEntryID {
			t.Fatalf("history journal id mismatch for transfer %s: %+v", transferID, row)
		}
		if (counterpartyAccountID == nil) != (row.CounterpartyAccountID == nil) {
			t.Fatalf("history counterparty mismatch for transfer %s: %+v", transferID, row)
		}
		if counterpartyAccountID != nil && *row.CounterpartyAccountID != *counterpartyAccountID {
			t.Fatalf("history counterparty id mismatch for transfer %s: %+v", transferID, row)
		}
		if row.InstitutionID != DemoInstitutionID || row.AccountID == "" || row.CurrencyID != "NGN" {
			t.Fatalf("history required fields mismatch for transfer %s: %+v", transferID, row)
		}
		return
	}
	t.Fatalf("missing history row for transfer %s in %+v", transferID, history)
}

func mustInternalCredit(t *testing.T, svc *Service, ctx context.Context, input InternalCreditInput) *Transfer {
	t.Helper()
	transfer, err := svc.InternalCredit(ctx, input)
	return mustTransfer(t, transfer, err)
}

func mustInternalDebit(t *testing.T, svc *Service, ctx context.Context, input InternalDebitInput) *Transfer {
	t.Helper()
	transfer, err := svc.InternalDebit(ctx, input)
	return mustTransfer(t, transfer, err)
}

func mustInternalTransfer(t *testing.T, svc *Service, ctx context.Context, input InternalTransferInput) *Transfer {
	t.Helper()
	transfer, err := svc.InternalTransfer(ctx, input)
	return mustTransfer(t, transfer, err)
}
