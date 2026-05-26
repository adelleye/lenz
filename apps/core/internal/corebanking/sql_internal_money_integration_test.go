//go:build integration

package corebanking

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestSQLRepositoryInternalCreditIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Internal",
		LastName:      "Credit",
		Email:         "internal.credit@example.com",
		Phone:         "+2348012345682",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567895",
		Name:          "Internal Credit Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}

	credit, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    25000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-credit-001",
		Reference:      "sql-internal-credit-ref-001",
		Narration:      "SQL internal credit proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, credit, TransferStatusSucceeded)
	if credit.Direction != TransferDirectionInbound || credit.Provider != ProviderLedgerInternal || credit.ProviderReference != "sql-internal-credit-ref-001" || credit.JournalEntryID == nil {
		t.Fatalf("internal credit transfer mismatch: %+v", credit)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 25000, 25000)
	assertSQLJournalBalanced(t, svc, ctx, credit, 25000)
	assertSQLInternalCreditRows(t, ctx, db, credit, account.ID, DemoClearingAccountID, 25000)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].TransferID != credit.ID || history[0].SignedAmountMinor != 25000 || history[0].JournalEntryID == nil {
		t.Fatalf("internal credit history mismatch: %+v", history)
	}

	duplicate, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    25000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-credit-001",
		Reference:      "sql-internal-credit-ref-001",
		Narration:      "SQL internal credit duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != credit.ID {
		t.Fatalf("duplicate idempotency key posted a new internal credit: first=%s duplicate=%s", credit.ID, duplicate.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-credit-001", 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalCreditConcurrentIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Concurrent",
		LastName:      "Credit",
		Email:         "concurrent.credit@example.com",
		Phone:         "+2348012345683",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567896",
		Name:          "Concurrent Credit Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}

	const amount = int64(30000)
	const idempotencyKey = "sql-internal-credit-concurrent"
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalCredit(ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    amount,
			CurrencyID:     "NGN",
			IdempotencyKey: idempotencyKey,
			Reference:      "sql-internal-credit-concurrent-ref",
			Narration:      fmt.Sprintf("SQL concurrent internal credit %02d", i),
		})
	})

	transfer := assertConcurrentReplay(t, results)
	assertStatus(t, transfer, TransferStatusSucceeded)
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, amount, amount)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, amount, amount)
	assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
	assertSQLInternalCreditRows(t, ctx, db, transfer, account.ID, DemoClearingAccountID, amount)
	assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalDebitIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	account := createSQLCustomerAccount(t, svc, ctx, "Internal", "Debit", "internal.debit@example.com", "1234567897", "Internal Debit Wallet")

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    40000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-fund",
		Narration:      "SQL internal debit funding",
	}); err != nil {
		t.Fatal(err)
	}

	debit, err := svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    15000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-001",
		Reference:      "sql-internal-debit-ref-001",
		Narration:      "SQL internal debit proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, debit, TransferStatusSucceeded)
	if debit.Direction != TransferDirectionOutbound || debit.Provider != ProviderLedgerInternal || debit.ProviderReference != "sql-internal-debit-ref-001" || debit.JournalEntryID == nil {
		t.Fatalf("internal debit transfer mismatch: %+v", debit)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 25000, 25000)
	assertSQLJournalBalanced(t, svc, ctx, debit, 15000)
	assertSQLInternalDebitRows(t, ctx, db, debit, account.ID, DemoClearingAccountID, 15000)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seenDebit := false
	for _, txn := range history {
		if txn.TransferID == debit.ID && txn.SignedAmountMinor == -15000 && txn.JournalEntryID != nil {
			seenDebit = true
		}
	}
	if !seenDebit {
		t.Fatalf("internal debit history mismatch: %+v", history)
	}

	duplicate, err := svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    15000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-001",
		Reference:      "sql-internal-debit-ref-001",
		Narration:      "SQL internal debit duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != debit.ID {
		t.Fatalf("duplicate idempotency key posted a new internal debit: first=%s duplicate=%s", debit.ID, duplicate.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-debit-001", 1)

	_, err = svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    99999,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-insufficient",
		Narration:      "SQL internal debit insufficient",
	})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-debit-insufficient", 0)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalDebitConcurrentIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	account := createSQLCustomerAccount(t, svc, ctx, "Concurrent", "Debit", "concurrent.debit@example.com", "1234567898", "Concurrent Debit Wallet")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-concurrent-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(10000)
	const idempotencyKey = "sql-internal-debit-concurrent"
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalDebit(ctx, InternalDebitInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    amount,
			CurrencyID:     "NGN",
			IdempotencyKey: idempotencyKey,
			Reference:      "sql-internal-debit-concurrent-ref",
			Narration:      fmt.Sprintf("SQL concurrent internal debit %02d", i),
		})
	})

	transfer := assertConcurrentReplay(t, results)
	assertStatus(t, transfer, TransferStatusSucceeded)
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 20000, 20000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 20000, 20000)
	assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
	assertSQLInternalDebitRows(t, ctx, db, transfer, account.ID, DemoClearingAccountID, amount)
	assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalDebitConcurrentDistinctNoOverspend(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	account := createSQLCustomerAccount(t, svc, ctx, "Distinct", "Debit", "distinct.debit@example.com", "1234567899", "Distinct Debit Wallet")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-distinct-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(7000)
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalDebit(ctx, InternalDebitInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    amount,
			CurrencyID:     "NGN",
			IdempotencyKey: fmt.Sprintf("sql-internal-debit-distinct-attempt-%02d", i),
			Narration:      fmt.Sprintf("SQL distinct internal debit %02d", i),
		})
	})

	successes := 0
	insufficient := 0
	for i, result := range results {
		switch {
		case result.err == nil:
			successes++
			assertStatus(t, result.transfer, TransferStatusSucceeded)
			assertSQLJournalBalanced(t, svc, ctx, result.transfer, amount)
		case errors.Is(result.err, ErrInsufficient):
			insufficient++
		default:
			t.Fatalf("unexpected distinct debit result %d: transfer=%+v err=%v", i, result.transfer, result.err)
		}
	}
	if successes != 4 || insufficient != 6 {
		t.Fatalf("expected 4 successful debits and 6 insufficient rejections, got successes=%d insufficient=%d", successes, insufficient)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 2000, 2000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 2000, 2000)
	assertSQLTransferCountByIdempotencyPrefix(t, ctx, db, "sql-internal-debit-distinct-attempt-", 4)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalTransferIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	source := createSQLCustomerAccount(t, svc, ctx, "Internal", "TransferSource", "internal.transfer.source@example.com", "2234567890", "Internal Transfer Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Internal", "TransferDestination", "internal.transfer.destination@example.com", "2234567891", "Internal Transfer Destination")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    40000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-transfer-fund",
		Narration:      "SQL internal transfer funding",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          15000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-internal-transfer-001",
		Reference:            "sql-internal-transfer-ref-001",
		Narration:            "SQL internal transfer proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, transfer, TransferStatusSucceeded)
	if transfer.AccountID != source.ID || transfer.Direction != TransferDirectionOutbound || transfer.Provider != ProviderLedgerInternal || transfer.ProviderReference != "sql-internal-transfer-ref-001" || transfer.JournalEntryID == nil {
		t.Fatalf("internal transfer mismatch: %+v", transfer)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 15000, 15000)
	assertSQLJournalBalanced(t, svc, ctx, transfer, 15000)
	assertSQLInternalTransferRows(t, ctx, db, transfer, source.ID, destination.ID, 15000)

	sourceHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, source.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceHistory) != 2 || sourceHistory[0].TransferID != transfer.ID || sourceHistory[0].SignedAmountMinor != -15000 || sourceHistory[0].Direction != TransactionDirectionDebit {
		t.Fatalf("source history mismatch: %+v", sourceHistory)
	}
	destinationHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, destination.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(destinationHistory) != 1 || destinationHistory[0].TransferID != transfer.ID || destinationHistory[0].SignedAmountMinor != 15000 || destinationHistory[0].Direction != TransactionDirectionCredit {
		t.Fatalf("destination history mismatch: %+v", destinationHistory)
	}

	duplicate, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          15000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-internal-transfer-001",
		Reference:            "sql-internal-transfer-ref-001",
		Narration:            "SQL internal transfer duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != transfer.ID {
		t.Fatalf("duplicate idempotency key posted a new internal transfer: first=%s duplicate=%s", transfer.ID, duplicate.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 15000, 15000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-transfer-001", 1)

	_, err = svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          99999,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-internal-transfer-insufficient",
		Narration:            "SQL internal transfer insufficient",
	})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 15000, 15000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-transfer-insufficient", 0)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalTransferReversalUsesOriginalCounterparty(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	source := createSQLCustomerAccount(t, svc, ctx, "Reverse", "SQLSource", "reverse.sql.source@example.com", "2234567896", "Reverse SQL Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Reverse", "SQLDestination", "reverse.sql.destination@example.com", "2234567897", "Reverse SQL Destination")
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    20000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-reverse-internal-fund",
	})
	original := mustInternalTransfer(t, svc, ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          7000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-reverse-internal-transfer",
		Reference:            "sql-reverse-internal-transfer-ref",
	})

	reversal := reverseTransfer(t, svc, ctx, original.ID, "sql-reverse-internal-transfer-reversal")

	if reversal.Direction != TransferDirectionReversal || reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != original.ID {
		t.Fatalf("SQL reversal did not reference original internal transfer: %+v", reversal)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 20000, 20000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 0, 0)
	assertSQLJournalBalanced(t, svc, ctx, reversal, 7000)
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepositoryInternalTransferConcurrentIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	source := createSQLCustomerAccount(t, svc, ctx, "Concurrent", "TransferSource", "concurrent.transfer.source@example.com", "2234567892", "Concurrent Transfer Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Concurrent", "TransferDestination", "concurrent.transfer.destination@example.com", "2234567893", "Concurrent Transfer Destination")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-transfer-concurrent-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(10000)
	const idempotencyKey = "sql-internal-transfer-concurrent"
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalTransfer(ctx, InternalTransferInput{
			InstitutionID:        DemoInstitutionID,
			SourceAccountID:      source.ID,
			DestinationAccountID: destination.ID,
			AmountMinor:          amount,
			CurrencyID:           "NGN",
			IdempotencyKey:       idempotencyKey,
			Reference:            "sql-internal-transfer-concurrent-ref",
			Narration:            fmt.Sprintf("SQL concurrent internal transfer %02d", i),
		})
	})

	transfer := assertConcurrentReplay(t, results)
	assertStatus(t, transfer, TransferStatusSucceeded)
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 20000, 20000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 10000, 10000)
	assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
	assertSQLInternalTransferRows(t, ctx, db, transfer, source.ID, destination.ID, amount)
	assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryIdempotencyRejectsChangedRequest(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	sourceA := createSQLCustomerAccount(t, svc, ctx, "Idem", "SourceA", "sql.idem.source.a@example.com", "2234567810", "SQL Idem Source A")
	sourceB := createSQLCustomerAccount(t, svc, ctx, "Idem", "SourceB", "sql.idem.source.b@example.com", "2234567811", "SQL Idem Source B")
	destinationA := createSQLCustomerAccount(t, svc, ctx, "Idem", "DestA", "sql.idem.dest.a@example.com", "2234567812", "SQL Idem Dest A")
	destinationB := createSQLCustomerAccount(t, svc, ctx, "Idem", "DestB", "sql.idem.dest.b@example.com", "2234567813", "SQL Idem Dest B")

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      sourceA.ID,
		AmountMinor:    100000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-idem-conflict-fund-a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      sourceB.ID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-idem-conflict-fund-b",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      destinationA.ID,
		AmountMinor:    1000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-idem-conflict-amount",
		Reference:      "sql-idem-conflict-credit-ref",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      destinationA.ID,
		AmountMinor:    2000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-idem-conflict-amount",
		Reference:      "sql-idem-conflict-credit-ref",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed amount to return conflict, got %v", err)
	}

	sameRequest := InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      sourceA.ID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          5000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-idem-replay-same",
		Reference:            "sql-idem-replay-same-ref",
	}
	first, err := svc.InternalTransfer(ctx, sameRequest)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.InternalTransfer(ctx, sameRequest)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID {
		t.Fatalf("same request replay returned transfer %s, want %s", replay.ID, first.ID)
	}

	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      sourceA.ID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          7000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-idem-conflict-destination",
		Reference:            "sql-idem-conflict-destination-ref",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      sourceA.ID,
		DestinationAccountID: destinationB.ID,
		AmountMinor:          7000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-idem-conflict-destination",
		Reference:            "sql-idem-conflict-destination-ref",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed destination to return conflict, got %v", err)
	}

	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      sourceA.ID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          3000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-idem-conflict-source",
		Reference:            "sql-idem-conflict-source-ref",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      sourceB.ID,
		DestinationAccountID: destinationA.ID,
		AmountMinor:          3000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-idem-conflict-source",
		Reference:            "sql-idem-conflict-source-ref",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed source to return conflict, got %v", err)
	}

	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-idem-conflict-amount", 1)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-idem-conflict-destination", 1)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-idem-conflict-source", 1)
}

func TestSQLRepositoryProviderEventRejectsChangedPayload(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	otherAccount := createSQLCustomerAccount(t, svc, ctx, "Provider", "Replay", "sql.provider.replay@example.com", "2234567814", "SQL Provider Replay")

	tests := []struct {
		name        string
		eventID     string
		first       TransferRequest
		second      TransferRequest
		wantBalance int64
	}{
		{
			name:    "amount",
			eventID: "sql-provider-payload-amount",
			first: TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     10000,
				IdempotencyKey:  "sql-provider-payload-amount-1",
				ProviderEventID: "sql-provider-payload-amount",
			},
			second: TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     20000,
				IdempotencyKey:  "sql-provider-payload-amount-2",
				ProviderEventID: "sql-provider-payload-amount",
			},
			wantBalance: 10000,
		},
		{
			name:    "account",
			eventID: "sql-provider-payload-account",
			first: TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     3000,
				IdempotencyKey:  "sql-provider-payload-account-1",
				ProviderEventID: "sql-provider-payload-account",
			},
			second: TransferRequest{
				AccountID:       otherAccount.ID,
				AmountMinor:     3000,
				IdempotencyKey:  "sql-provider-payload-account-2",
				ProviderEventID: "sql-provider-payload-account",
			},
			wantBalance: 13000,
		},
		{
			name:    "status",
			eventID: "sql-provider-payload-status",
			first: TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     5000,
				IdempotencyKey:  "sql-provider-payload-status-1",
				ProviderEventID: "sql-provider-payload-status",
			},
			second: TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     5000,
				IdempotencyKey:  "sql-provider-payload-status-2",
				ProviderEventID: "sql-provider-payload-status",
				Status:          TransferStatusFailed,
			},
			wantBalance: 18000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.MockInbound(ctx, tt.first); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.MockInbound(ctx, tt.second); !errors.Is(err, ErrConflict) {
				t.Fatalf("expected changed provider-event %s to return conflict, got %v", tt.name, err)
			}
			assertSQLTransferCountByProviderEvent(t, ctx, db, tt.eventID, 1)
			assertSQLBalance(t, svc, ctx, tt.wantBalance)
		})
	}
	assertSQLAccountBalancePair(t, svc, ctx, otherAccount.ID, 0, 0)
}

func TestSQLRepositoryInternalTransferConcurrentDistinctNoOverspend(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	source := createSQLCustomerAccount(t, svc, ctx, "Distinct", "TransferSource", "distinct.transfer.source@example.com", "2234567894", "Distinct Transfer Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Distinct", "TransferDestination", "distinct.transfer.destination@example.com", "2234567895", "Distinct Transfer Destination")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-transfer-distinct-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(7000)
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalTransfer(ctx, InternalTransferInput{
			InstitutionID:        DemoInstitutionID,
			SourceAccountID:      source.ID,
			DestinationAccountID: destination.ID,
			AmountMinor:          amount,
			CurrencyID:           "NGN",
			IdempotencyKey:       fmt.Sprintf("sql-internal-transfer-distinct-attempt-%02d", i),
			Narration:            fmt.Sprintf("SQL distinct internal transfer %02d", i),
		})
	})

	successes := 0
	insufficient := 0
	for i, result := range results {
		switch {
		case result.err == nil:
			successes++
			assertStatus(t, result.transfer, TransferStatusSucceeded)
			assertSQLJournalBalanced(t, svc, ctx, result.transfer, amount)
		case errors.Is(result.err, ErrInsufficient):
			insufficient++
		default:
			t.Fatalf("unexpected distinct internal transfer result %d: transfer=%+v err=%v", i, result.transfer, result.err)
		}
	}
	if successes != 4 || insufficient != 6 {
		t.Fatalf("expected 4 successful transfers and 6 insufficient rejections, got successes=%d insufficient=%d", successes, insufficient)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 2000, 2000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 28000, 28000)
	assertSQLTransferCountByIdempotencyPrefix(t, ctx, db, "sql-internal-transfer-distinct-attempt-", 4)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryAccountCreateConcurrentDuplicateNumber(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Concurrent",
		LastName:      "Account",
		Email:         "concurrent.account@example.com",
		Phone:         "+2348012345680",
	})
	if err != nil {
		t.Fatal(err)
	}

	const accountNumber = "1234567892"
	const requestCount = 10
	start := make(chan struct{})
	results := make(chan error, requestCount)
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CreateAccount(ctx, CreateAccountInput{
				InstitutionID: DemoInstitutionID,
				CustomerID:    customer.ID,
				AccountNumber: accountNumber,
				Name:          "Concurrent Wallet",
				ProductType:   AccountProductStandardWallet,
				CurrencyID:    "NGN",
			})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent account create error: %v", err)
		}
	}
	if successes != 1 || conflicts != requestCount-1 {
		t.Fatalf("expected one success and %d conflicts, got successes=%d conflicts=%d", requestCount-1, successes, conflicts)
	}

	var accountRows int
	if err := db.GetContext(ctx, &accountRows, `SELECT COUNT(*) FROM accounts WHERE institution_id = $1 AND account_number = $2`, DemoInstitutionID, accountNumber); err != nil {
		t.Fatal(err)
	}
	if accountRows != 1 {
		t.Fatalf("expected one account row for duplicate account number, got %d", accountRows)
	}

	var balanceRows int
	if err := db.GetContext(ctx, &balanceRows, `
SELECT COUNT(*)
FROM account_balances b
JOIN accounts a ON a.institution_id = b.institution_id AND a.id = b.account_id
WHERE a.institution_id = $1 AND a.account_number = $2`, DemoInstitutionID, accountNumber); err != nil {
		t.Fatal(err)
	}
	if balanceRows != 1 {
		t.Fatalf("expected one balance row for duplicate account number, got %d", balanceRows)
	}
}
