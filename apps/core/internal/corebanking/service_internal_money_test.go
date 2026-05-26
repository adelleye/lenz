package corebanking

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestInternalCreditPostsBalancedLedgerAndHistory(t *testing.T) {
	ctx, svc, store := newTestService(t)

	transfer, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-credit-001",
		Reference:      "internal-credit-ref-001",
		Narration:      "cash deposit",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, transfer, TransferStatusSucceeded)
	if transfer.Direction != TransferDirectionInbound || transfer.Provider != ProviderLedgerInternal || transfer.ProviderReference != "internal-credit-ref-001" || transfer.Narration != "cash deposit" {
		t.Fatalf("internal credit transfer metadata mismatch: %+v", transfer)
	}
	if transfer.ProviderStatus != TransferStatusSucceeded || transfer.LedgerStatus != LedgerStatusPosted || transfer.ReconciliationStatus != ReconciliationStatusMatched {
		t.Fatalf("internal credit statuses mismatch: %+v", transfer)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoClearingAccountID, 10000)
	assertJournalBalanced(t, store, transfer)

	journal, err := store.GetJournal(ctx, DemoInstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	postings := map[string]string{}
	for _, posting := range journal.Postings {
		postings[posting.AccountID] = posting.Direction
	}
	if postings[DemoClearingAccountID] != PostingDebit || postings[DemoCustomerAccountID] != PostingCredit {
		t.Fatalf("internal credit postings should debit source and credit customer: %+v", journal.Postings)
	}

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].TransferID != transfer.ID || history[0].SignedAmountMinor != 10000 || history[0].JournalEntryID == nil {
		t.Fatalf("internal credit history mismatch: %+v", history)
	}
}

func TestInternalCreditIdempotencyDoesNotDoubleCredit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	input := InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-credit-idem",
		Narration:      "idempotent cash deposit",
	}

	first, err := svc.InternalCredit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.InternalCredit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate idempotency key posted a new transfer: first=%s second=%s", first.ID, second.ID)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
}

func TestInternalCreditRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		input   InternalCreditInput
		mutate  func(*memoryStore)
		wantErr error
	}{
		{
			name:    "missing institution",
			input:   InternalCreditInput{AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "missing-institution"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "invalid account id",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: "not-a-uuid", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "invalid-account"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "missing account",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: "99999999-9999-9999-9999-999999999999", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "missing-account"},
			wantErr: ErrNotFound,
		},
		{
			name:    "cross institution account",
			input:   InternalCreditInput{InstitutionID: "99999999-9999-9999-9999-999999999999", AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "cross-institution"},
			wantErr: ErrNotFound,
		},
		{
			name:    "zero amount",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 0, CurrencyID: "NGN", IdempotencyKey: "zero-amount"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "unsupported currency",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "USD", IdempotencyKey: "bad-currency"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "missing idempotency",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "closed customer account",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "closed-account"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoCustomerAccountID, "closed") },
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "frozen customer account",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "frozen-account"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoCustomerAccountID, "frozen") },
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "customer account as source",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, SourceAccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "customer-source"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "no safe default source",
			input:   InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "no-safe-source"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoClearingAccountID, "closed") },
			wantErr: ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, store := newTestService(t)
			if tt.mutate != nil {
				tt.mutate(store)
			}
			_, err := svc.InternalCredit(ctx, tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestInternalDebitPostsBalancedLedgerAndHistory(t *testing.T) {
	ctx, svc, store := newTestService(t)
	_, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-debit-fund-001",
	})
	if err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    12000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-debit-001",
		Reference:      "internal-debit-ref-001",
		Narration:      "cash withdrawal",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, transfer, TransferStatusSucceeded)
	if transfer.Direction != TransferDirectionOutbound || transfer.Provider != ProviderLedgerInternal || transfer.ProviderReference != "internal-debit-ref-001" || transfer.Narration != "cash withdrawal" {
		t.Fatalf("internal debit transfer metadata mismatch: %+v", transfer)
	}
	if transfer.ProviderStatus != TransferStatusSucceeded || transfer.LedgerStatus != LedgerStatusPosted || transfer.ReconciliationStatus != ReconciliationStatusMatched {
		t.Fatalf("internal debit statuses mismatch: %+v", transfer)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 38000)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoClearingAccountID, 38000)
	assertJournalBalanced(t, store, transfer)

	journal, err := store.GetJournal(ctx, DemoInstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	postings := map[string]string{}
	for _, posting := range journal.Postings {
		postings[posting.AccountID] = posting.Direction
	}
	if postings[DemoCustomerAccountID] != PostingDebit || postings[DemoClearingAccountID] != PostingCredit {
		t.Fatalf("internal debit postings should debit customer and credit destination: %+v", journal.Postings)
	}

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].TransferID != transfer.ID || history[0].SignedAmountMinor != -12000 || history[0].JournalEntryID == nil {
		t.Fatalf("internal debit history mismatch: %+v", history)
	}
}

func TestInternalDebitIdempotencyDoesNotDoubleDebit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	_, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-debit-idem-fund",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-debit-idem",
		Narration:      "idempotent cash withdrawal",
	}

	first, err := svc.InternalDebit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.InternalDebit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate idempotency key posted a new transfer: first=%s second=%s", first.ID, second.ID)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 20000)
}

func TestInternalDebitRejectsInsufficientFundsWithoutTransfer(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	_, err := svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-debit-no-funds",
	})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)

	transfers, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, transfer := range transfers {
		if transfer.IdempotencyKey == "internal-debit-no-funds" {
			t.Fatalf("insufficient internal debit should not create transfer: %+v", transfer)
		}
	}
}

func TestInternalDebitRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		input   InternalDebitInput
		mutate  func(*memoryStore)
		wantErr error
	}{
		{
			name:    "missing institution",
			input:   InternalDebitInput{AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-missing-institution"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "invalid account id",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: "not-a-uuid", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-invalid-account"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "missing account",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: "99999999-9999-9999-9999-999999999999", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-missing-account"},
			wantErr: ErrNotFound,
		},
		{
			name:    "cross institution account",
			input:   InternalDebitInput{InstitutionID: "99999999-9999-9999-9999-999999999999", AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-cross-institution"},
			wantErr: ErrNotFound,
		},
		{
			name:    "zero amount",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 0, CurrencyID: "NGN", IdempotencyKey: "debit-zero-amount"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "unsupported currency",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "USD", IdempotencyKey: "debit-bad-currency"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "missing idempotency",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "closed customer account",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-closed-account"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoCustomerAccountID, "closed") },
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "frozen customer account",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-frozen-account"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoCustomerAccountID, "frozen") },
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "post no debit customer account",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-pnd-account"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoCustomerAccountID, "post_no_debit") },
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "customer account as destination",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, DestinationAccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-customer-destination"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "invalid destination account id",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, DestinationAccountID: "not-a-uuid", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-invalid-destination"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "no safe default destination",
			input:   InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "debit-no-safe-destination"},
			mutate:  func(store *memoryStore) { setMemoryAccountStatus(store, DemoClearingAccountID, "closed") },
			wantErr: ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, store := newTestService(t)
			if tt.mutate != nil {
				tt.mutate(store)
			}
			_, err := svc.InternalDebit(ctx, tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestInternalTransferPostsBalancedLedgerAndBothHistories(t *testing.T) {
	ctx, svc, store := newTestService(t)
	destination := createMemoryCustomerAccount(t, svc, ctx, "Transfer", "Receiver", "transfer.receiver@example.com", "9990000002")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-transfer-fund-001",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destination.ID,
		AmountMinor:          12000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "internal-transfer-001",
		Reference:            "internal-transfer-ref-001",
		Narration:            "wallet to wallet",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, transfer, TransferStatusSucceeded)
	if transfer.AccountID != DemoCustomerAccountID || transfer.Direction != TransferDirectionOutbound || transfer.Provider != ProviderLedgerInternal || transfer.ProviderReference != "internal-transfer-ref-001" {
		t.Fatalf("internal transfer metadata mismatch: %+v", transfer)
	}
	if transfer.ProviderStatus != TransferStatusSucceeded || transfer.LedgerStatus != LedgerStatusPosted || transfer.ReconciliationStatus != ReconciliationStatusMatched {
		t.Fatalf("internal transfer statuses mismatch: %+v", transfer)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 38000)
	assertBalance(t, svc, ctx, DemoInstitutionID, destination.ID, 12000)
	assertJournalBalanced(t, store, transfer)

	journal, err := store.GetJournal(ctx, DemoInstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	postings := map[string]string{}
	for _, posting := range journal.Postings {
		postings[posting.AccountID] = posting.Direction
	}
	if postings[DemoCustomerAccountID] != PostingDebit || postings[destination.ID] != PostingCredit {
		t.Fatalf("internal transfer postings should debit source and credit destination: %+v", journal.Postings)
	}

	sourceHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceHistory) != 2 || sourceHistory[0].TransferID != transfer.ID || sourceHistory[0].SignedAmountMinor != -12000 || sourceHistory[0].Direction != TransactionDirectionDebit {
		t.Fatalf("source history mismatch: %+v", sourceHistory)
	}
	destinationHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, destination.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(destinationHistory) != 1 || destinationHistory[0].TransferID != transfer.ID || destinationHistory[0].SignedAmountMinor != 12000 || destinationHistory[0].Direction != TransactionDirectionCredit {
		t.Fatalf("destination history mismatch: %+v", destinationHistory)
	}
}

func TestInternalTransferIdempotencyDoesNotDoublePost(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	destination := createMemoryCustomerAccount(t, svc, ctx, "Idem", "Receiver", "transfer.idem@example.com", "9990000003")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "internal-transfer-idem-fund",
	}); err != nil {
		t.Fatal(err)
	}
	input := InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destination.ID,
		AmountMinor:          10000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "internal-transfer-idem",
		Narration:            "idempotent wallet transfer",
	}

	first, err := svc.InternalTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.InternalTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate idempotency key posted a new transfer: first=%s second=%s", first.ID, second.ID)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 20000)
	assertBalance(t, svc, ctx, DemoInstitutionID, destination.ID, 10000)
}

func TestIdempotencyRejectsChangedMoneyRequest(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	destinationA := createMemoryCustomerAccount(t, svc, ctx, "Idem", "DestA", "transfer.idem.a@example.com", uniqueAccountNumber("72"))
	destinationB := createMemoryCustomerAccount(t, svc, ctx, "Idem", "DestB", "transfer.idem.b@example.com", uniqueAccountNumber("73"))
	sourceB := createMemoryCustomerAccount(t, svc, ctx, "Idem", "SourceB", "transfer.idem.source@example.com", uniqueAccountNumber("74"))

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "idempotency-conflict-fund-a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      sourceB.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "idempotency-conflict-fund-b",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      destinationA.ID,
		AmountMinor:    1000,
		CurrencyID:     "NGN",
		IdempotencyKey: "idempotency-conflict-amount",
		Reference:      "idempotency-conflict-credit-ref",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      destinationA.ID,
		AmountMinor:    2000,
		CurrencyID:     "NGN",
		IdempotencyKey: "idempotency-conflict-amount",
		Reference:      "idempotency-conflict-credit-ref",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed amount to return conflict, got %v", err)
	}

	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          5000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "idempotency-conflict-destination",
		Reference:            "idempotency-conflict-transfer-ref",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destinationB.ID,
		AmountMinor:          5000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "idempotency-conflict-destination",
		Reference:            "idempotency-conflict-transfer-ref",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed destination to return conflict, got %v", err)
	}

	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          3000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "idempotency-conflict-source",
		Reference:            "idempotency-conflict-source-ref",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      sourceB.ID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          3000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "idempotency-conflict-source",
		Reference:            "idempotency-conflict-source-ref",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed source to return conflict, got %v", err)
	}
}

func TestInternalTransferRejectsInsufficientFundsWithoutTransfer(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	destination := createMemoryCustomerAccount(t, svc, ctx, "NoFunds", "Receiver", "transfer.nofunds@example.com", "9990000004")
	_, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destination.ID,
		AmountMinor:          10000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "internal-transfer-no-funds",
	})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
	assertBalance(t, svc, ctx, DemoInstitutionID, destination.ID, 0)

	transfers, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, transfer := range transfers {
		if transfer.IdempotencyKey == "internal-transfer-no-funds" {
			t.Fatalf("insufficient internal transfer should not create transfer: %+v", transfer)
		}
	}
}

func TestInternalTransferRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		input   func(destinationID string) InternalTransferInput
		mutate  func(*memoryStore, string)
		wantErr error
	}{
		{
			name: "missing institution",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-missing-institution"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "invalid source account id",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: "not-a-uuid", DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-invalid-source"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "invalid destination account id",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: "not-a-uuid", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-invalid-destination"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "missing source account",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: "99999999-9999-9999-9999-999999999999", DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-missing-source"}
			},
			wantErr: ErrNotFound,
		},
		{
			name: "missing destination account",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: "99999999-9999-9999-9999-999999999999", AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-missing-destination"}
			},
			wantErr: ErrNotFound,
		},
		{
			name: "same source and destination",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-same-account"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "cross institution account",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: "99999999-9999-9999-9999-999999999999", SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-cross-institution"}
			},
			wantErr: ErrNotFound,
		},
		{
			name: "zero amount",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 0, CurrencyID: "NGN", IdempotencyKey: "transfer-zero-amount"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "unsupported currency",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "USD", IdempotencyKey: "transfer-bad-currency"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "missing idempotency",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN"}
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "closed source account",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-closed-source"}
			},
			mutate: func(store *memoryStore, destinationID string) {
				setMemoryAccountStatus(store, DemoCustomerAccountID, "closed")
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "frozen destination account",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-frozen-destination"}
			},
			mutate:  func(store *memoryStore, destinationID string) { setMemoryAccountStatus(store, destinationID, "frozen") },
			wantErr: ErrInvalidRequest,
		},
		{
			name: "cross currency account",
			input: func(destinationID string) InternalTransferInput {
				return InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: destinationID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "transfer-cross-currency"}
			},
			mutate:  func(store *memoryStore, destinationID string) { setMemoryAccountCurrency(store, destinationID, "USD") },
			wantErr: ErrInvalidRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, store := newTestService(t)
			destination := createMemoryCustomerAccount(t, svc, ctx, "Invalid", "Receiver", "transfer.invalid."+uuid.NewString()+"@example.com", uniqueAccountNumber("71"))
			if tt.mutate != nil {
				tt.mutate(store, destination.ID)
			}
			_, err := svc.InternalTransfer(ctx, tt.input(destination.ID))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}
