package corebanking

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestSuccessfulTransferInPostsBalancedLedger(t *testing.T) {
	ctx, svc, store := newTestService(t)
	transfer := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     50000,
		IdempotencyKey:  "in-1",
		ProviderEventID: "evt-in-1",
		Narration:       "inbound",
	})

	assertStatus(t, transfer, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	assertJournalBalanced(t, store, transfer)
}

func TestSuccessfulTransferOutPostsBalancedLedger(t *testing.T) {
	ctx, svc, store := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 80000, IdempotencyKey: "fund", ProviderEventID: "evt-fund"})

	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    30000,
		IdempotencyKey: "out-1",
		Narration:      "outbound",
	})

	assertStatus(t, transfer, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	assertJournalBalanced(t, store, transfer)
}

func TestInsufficientFundsDoesNotDebit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "out-insufficient",
	})

	assertStatus(t, transfer, TransferStatusFailed)
	if transfer.JournalEntryID != nil {
		t.Fatalf("failed transfer should not have journal entry")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
}

func TestDuplicateIdempotencyKeyDoesNotDoublePost(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	req := TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "idem-1", ProviderEventID: "evt-idem-1"}
	first := mockInbound(t, svc, ctx, req)
	second := mockInbound(t, svc, ctx, req)

	if first.ID != second.ID {
		t.Fatalf("expected duplicate idempotency request to return original transfer")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
}

func TestDuplicateProviderEventDoesNotDoubleCredit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	first := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "idem-provider-1", ProviderEventID: "evt-provider"})
	second := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "idem-provider-2", ProviderEventID: "evt-provider"})

	if first.ID != second.ID {
		t.Fatalf("expected duplicate provider event to return original transfer")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
}

func TestPendingTransferAppearsInHistory(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	transfer := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     10000,
		IdempotencyKey:  "pending-1",
		ProviderEventID: "evt-pending-1",
		Status:          TransferStatusPending,
	})

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].TransferID != transfer.ID || history[0].Status != TransferStatusPending || history[0].SignedMinor != 0 {
		t.Fatalf("pending history row mismatch: %+v", history)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
}

func TestFailedTransferDoesNotLoseMoney(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "fund-failed", ProviderEventID: "evt-fund-failed"})
	transfer := mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "failed-1", Status: TransferStatusFailed})

	assertStatus(t, transfer, TransferStatusFailed)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	if transfer.JournalEntryID != nil {
		t.Fatalf("failed transfer should not post a journal")
	}
}

func TestReversalCreatesNewLedgerEvent(t *testing.T) {
	ctx, svc, store := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 25000, IdempotencyKey: "rev-in", ProviderEventID: "evt-rev-in"})

	reversal := reverseTransfer(t, svc, ctx, original.ID, "reverse-1")

	if reversal.ID == original.ID || reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != original.ID {
		t.Fatalf("reversal did not reference original: %+v", reversal)
	}
	if reversal.JournalEntryID == nil || *reversal.JournalEntryID == *original.JournalEntryID {
		t.Fatalf("reversal should create a distinct journal entry")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
	assertJournalBalanced(t, store, reversal)
}

func TestReversalPostsEvenWhenFundsWereSpent(t *testing.T) {
	ctx, svc, store := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "spent-rev-in", ProviderEventID: "evt-spent-rev-in"})
	mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 20000, IdempotencyKey: "spent-rev-out"})

	reversal := reverseTransfer(t, svc, ctx, original.ID, "spent-reverse-1")

	assertStatus(t, reversal, TransferStatusSucceeded)
	if reversal.JournalEntryID == nil {
		t.Fatalf("spent-funds reversal should still create a journal")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, -20000)
	assertJournalBalanced(t, store, reversal)
}

func TestTenantScopingPreventsCrossTenantReads(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "tenant-in", ProviderEventID: "evt-tenant-in"})

	if _, err := svc.GetBalance(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant balance read to fail, got %v", err)
	}
	if _, err := svc.GetTransactions(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID); err != nil {
		t.Fatalf("empty cross-tenant history should not error: %v", err)
	}
}

func TestTransactionHistoryComesFromLenzRecords(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	inbound := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 12000, IdempotencyKey: "hist-in", ProviderEventID: "evt-hist-in"})
	outbound := mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 2000, IdempotencyKey: "hist-out"})

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("expected two Lenz transaction records, got %d", len(history))
	}
	ids := map[string]bool{history[0].TransferID: true, history[1].TransferID: true}
	if !ids[inbound.ID] || !ids[outbound.ID] {
		t.Fatalf("history did not reference Lenz transfer IDs: %+v", history)
	}
	for _, txn := range history {
		if txn.JournalEntryID == nil {
			t.Fatalf("succeeded history row must come from Lenz journal/posting record: %+v", txn)
		}
	}
}

func newTestService(t *testing.T) (context.Context, *Service, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	svc := NewService(store)
	ctx := context.Background()
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, svc, store
}

func mustTransfer(t *testing.T, transfer *Transfer, err error) *Transfer {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func mockInbound(t *testing.T, svc *Service, ctx context.Context, req TransferRequest) *Transfer {
	t.Helper()
	transfer, err := svc.MockInbound(ctx, req)
	return mustTransfer(t, transfer, err)
}

func mockOutbound(t *testing.T, svc *Service, ctx context.Context, req TransferRequest) *Transfer {
	t.Helper()
	transfer, err := svc.MockOutbound(ctx, req)
	return mustTransfer(t, transfer, err)
}

func reverseTransfer(t *testing.T, svc *Service, ctx context.Context, transferID, idempotencyKey string) *Transfer {
	t.Helper()
	transfer, err := svc.ReverseTransfer(ctx, DemoInstitutionID, transferID, idempotencyKey)
	return mustTransfer(t, transfer, err)
}

func assertStatus(t *testing.T, transfer *Transfer, status string) {
	t.Helper()
	if transfer.Status != status {
		t.Fatalf("expected status %s, got %s", status, transfer.Status)
	}
}

func assertBalance(t *testing.T, svc *Service, ctx context.Context, institutionID, accountID string, want int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, institutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != want || balance.LedgerMinor != want {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want=%d", balance.AvailableMinor, balance.LedgerMinor, want)
	}
}

func assertJournalBalanced(t *testing.T, store *memoryStore, transfer *Transfer) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected transfer to have journal entry")
	}
	journal, err := store.GetJournal(context.Background(), transfer.InstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Balanced || len(journal.Postings) != 2 {
		t.Fatalf("journal not balanced: %+v", journal)
	}
}

type memoryStore struct {
	mu             sync.Mutex
	institutions   map[string]Institution
	branches       map[string]Branch
	customers      map[string]Customer
	accounts       map[string]Account
	balances       map[string]AccountBalance
	transfers      map[string]Transfer
	journals       map[string]JournalEntry
	postings       map[string][]Posting
	idempotency    map[string]string
	providerEvents map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		institutions:   map[string]Institution{},
		branches:       map[string]Branch{},
		customers:      map[string]Customer{},
		accounts:       map[string]Account{},
		balances:       map[string]AccountBalance{},
		transfers:      map[string]Transfer{},
		journals:       map[string]JournalEntry{},
		postings:       map[string][]Posting{},
		idempotency:    map[string]string{},
		providerEvents: map[string]string{},
	}
}

func (m *memoryStore) EnsureDemoData(ctx context.Context) (*SeedResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	customerID := DemoCustomerID
	m.institutions[DemoInstitutionID] = Institution{ID: DemoInstitutionID, Name: "Lenz Demo Microfinance Bank", ShortName: "Lenz Demo", Code: "999001", CurrencyID: "NGN", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.branches[DemoBranchID] = Branch{ID: DemoBranchID, InstitutionID: DemoInstitutionID, Code: "HQ", Name: "Demo HQ", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.customers[DemoCustomerID] = Customer{ID: DemoCustomerID, InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, FirstName: "Ada", LastName: "Demo", Email: "ada.demo@example.com", Phone: "+2348000000000", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.accounts[DemoCustomerAccountID] = Account{ID: DemoCustomerAccountID, InstitutionID: DemoInstitutionID, CustomerID: &customerID, AccountNumber: "9990000001", Name: "Ada Demo Wallet", Kind: AccountKindCustomer, CurrencyID: "NGN", NormalBalance: NormalBalanceCredit, Status: "active", CreatedAt: now, UpdatedAt: now}
	m.accounts[DemoClearingAccountID] = Account{ID: DemoClearingAccountID, InstitutionID: DemoInstitutionID, AccountNumber: "9999999999", Name: "Mock NIP Clearing", Kind: AccountKindInternal, CurrencyID: "NGN", NormalBalance: NormalBalanceDebit, Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, ok := m.balances[DemoCustomerAccountID]; !ok {
		m.balances[DemoCustomerAccountID] = AccountBalance{AccountID: DemoCustomerAccountID, InstitutionID: DemoInstitutionID, CurrencyID: "NGN", UpdatedAt: now}
	}
	if _, ok := m.balances[DemoClearingAccountID]; !ok {
		m.balances[DemoClearingAccountID] = AccountBalance{AccountID: DemoClearingAccountID, InstitutionID: DemoInstitutionID, CurrencyID: "NGN", UpdatedAt: now}
	}
	return m.seedResultLocked(), nil
}

func (m *memoryStore) seedResultLocked() *SeedResult {
	return &SeedResult{
		Institution: m.institutions[DemoInstitutionID],
		Branch:      m.branches[DemoBranchID],
		Customer:    m.customers[DemoCustomerID],
		Account:     m.accounts[DemoCustomerAccountID],
		Clearing:    m.accounts[DemoClearingAccountID],
	}
}

func (m *memoryStore) ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var accounts []Account
	for _, account := range m.accounts {
		if account.InstitutionID == institutionID && account.CustomerID != nil && *account.CustomerID == customerID {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (m *memoryStore) GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	account, ok := m.accounts[accountID]
	if !ok || account.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(account), nil
}

func (m *memoryStore) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	balance, ok := m.balances[accountID]
	if !ok || balance.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(balance), nil
}

func (m *memoryStore) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[transferID]
	if !ok || transfer.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(transfer), nil
}

func (m *memoryStore) ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var transfers []Transfer
	for _, transfer := range m.transfers {
		if transfer.InstitutionID == institutionID {
			transfers = append(transfers, transfer)
		}
	}
	sort.Slice(transfers, func(i, j int) bool { return transfers[i].CreatedAt.After(transfers[j].CreatedAt) })
	return transfers, nil
}

func (m *memoryStore) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	journal, ok := m.journals[journalEntryID]
	if !ok || journal.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return &JournalWithPostings{JournalEntry: journal, Postings: append([]Posting(nil), m.postings[journalEntryID]...), Balanced: journal.TotalDebitMinor == journal.TotalCreditMinor}, nil
}

func (m *memoryStore) ListTransactions(ctx context.Context, institutionID, accountID string) ([]Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var txns []Transaction
	for _, transfer := range m.transfers {
		if transfer.InstitutionID != institutionID || transfer.AccountID != accountID {
			continue
		}
		signed := int64(0)
		if transfer.Status == TransferStatusSucceeded && transfer.JournalEntryID != nil {
			for _, posting := range m.postings[*transfer.JournalEntryID] {
				if posting.AccountID == accountID {
					account := m.accounts[accountID]
					if (account.NormalBalance == NormalBalanceCredit && posting.Direction == PostingCredit) || (account.NormalBalance == NormalBalanceDebit && posting.Direction == PostingDebit) {
						signed = posting.AmountMinor
					} else {
						signed = -posting.AmountMinor
					}
				}
			}
		}
		txns = append(txns, Transaction{ID: transfer.ID, TransferID: transfer.ID, JournalEntryID: transfer.JournalEntryID, AccountID: accountID, Direction: transfer.Direction, Status: transfer.Status, AmountMinor: transfer.AmountMinor, SignedMinor: signed, CurrencyID: transfer.CurrencyID, Narration: transfer.Narration, CreatedAt: transfer.CreatedAt})
	}
	sort.Slice(txns, func(i, j int) bool { return txns[i].CreatedAt.After(txns[j].CreatedAt) })
	return txns, nil
}

func (m *memoryStore) RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recordTransferLocked(input)
}

func (m *memoryStore) ReverseTransfer(ctx context.Context, institutionID, transferID, idempotencyKey string) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	original, ok := m.transfers[transferID]
	if !ok || original.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	if original.Status != TransferStatusSucceeded || original.JournalEntryID == nil || original.Direction == TransferDirectionReversal {
		return nil, ErrInvalidRequest
	}
	direction := TransferDirectionOutbound
	if original.Direction == TransferDirectionOutbound {
		direction = TransferDirectionInbound
	}
	reversal, err := m.recordTransferLocked(RecordTransferInput{InstitutionID: institutionID, AccountID: original.AccountID, ClearingAccountID: DemoClearingAccountID, Direction: direction, Status: TransferStatusSucceeded, AmountMinor: original.AmountMinor, CurrencyID: original.CurrencyID, IdempotencyKey: idempotencyKey, Provider: ProviderMockNIP, ProviderReference: "reversal:" + original.ID, ReversalOfTransferID: original.ID, Narration: "Reversal of " + original.ID})
	if err != nil {
		return nil, err
	}
	reversal.Direction = TransferDirectionReversal
	m.transfers[reversal.ID] = *reversal
	return reversal, nil
}

func (m *memoryStore) recordTransferLocked(input RecordTransferInput) (*Transfer, error) {
	if id := m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey]; id != "" {
		return copyOf(m.transfers[id]), nil
	}
	if input.ProviderEventID != "" {
		if id := m.providerEvents[input.InstitutionID+"|"+input.Provider+"|"+input.ProviderEventID]; id != "" {
			return copyOf(m.transfers[id]), nil
		}
	}
	account, ok := m.accounts[input.AccountID]
	if !ok || account.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if _, ok = m.accounts[input.ClearingAccountID]; !ok {
		return nil, ErrNotFound
	}
	status := input.Status
	failureReason := input.FailureReason
	if status == TransferStatusSucceeded && input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == "" && m.balances[input.AccountID].AvailableMinor < input.AmountMinor {
		status = TransferStatusFailed
		failureReason = "insufficient_funds"
	}
	now := time.Now().UTC()
	transfer := Transfer{ID: newID(), InstitutionID: input.InstitutionID, AccountID: input.AccountID, Direction: input.Direction, Status: status, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, IdempotencyKey: input.IdempotencyKey, Provider: input.Provider, ProviderReference: input.ProviderReference, Narration: input.Narration, CreatedAt: now, UpdatedAt: now}
	if input.ProviderEventID != "" {
		transfer.ProviderEventID = &input.ProviderEventID
	}
	if input.ReversalOfTransferID != "" {
		transfer.ReversalOfTransferID = &input.ReversalOfTransferID
	}
	if failureReason != "" {
		transfer.FailureReason = &failureReason
	}
	if status == TransferStatusSucceeded {
		journalID := m.postJournalLocked(input, transfer.ID, now)
		transfer.JournalEntryID = &journalID
	}
	m.transfers[transfer.ID] = transfer
	m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey] = transfer.ID
	if input.ProviderEventID != "" {
		m.providerEvents[input.InstitutionID+"|"+input.Provider+"|"+input.ProviderEventID] = transfer.ID
	}
	return copyOf(transfer), nil
}

func (m *memoryStore) postJournalLocked(input RecordTransferInput, transferID string, now time.Time) string {
	journalID := newID()
	entryType := input.Direction
	if input.ReversalOfTransferID != "" {
		entryType = TransferDirectionReversal
	}
	journal := JournalEntry{ID: journalID, InstitutionID: input.InstitutionID, TransferID: &transferID, EntryType: entryType, CurrencyID: input.CurrencyID, Narration: input.Narration, TotalDebitMinor: input.AmountMinor, TotalCreditMinor: input.AmountMinor, CreatedAt: now}
	m.journals[journalID] = journal
	debitAccountID := input.ClearingAccountID
	creditAccountID := input.AccountID
	if input.Direction == TransferDirectionOutbound {
		debitAccountID = input.AccountID
		creditAccountID = input.ClearingAccountID
	}
	m.postings[journalID] = []Posting{
		{ID: newID(), InstitutionID: input.InstitutionID, JournalEntryID: journalID, AccountID: debitAccountID, Direction: PostingDebit, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, CreatedAt: now},
		{ID: newID(), InstitutionID: input.InstitutionID, JournalEntryID: journalID, AccountID: creditAccountID, Direction: PostingCredit, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, CreatedAt: now},
	}
	for _, posting := range m.postings[journalID] {
		m.applyPostingLocked(posting, journalID, now)
	}
	return journalID
}

func (m *memoryStore) applyPostingLocked(posting Posting, journalID string, now time.Time) {
	account := m.accounts[posting.AccountID]
	delta := -posting.AmountMinor
	if (account.NormalBalance == NormalBalanceDebit && posting.Direction == PostingDebit) || (account.NormalBalance == NormalBalanceCredit && posting.Direction == PostingCredit) {
		delta = posting.AmountMinor
	}
	balance := m.balances[posting.AccountID]
	balance.AvailableMinor += delta
	balance.LedgerMinor += delta
	balance.LastJournalEntryID = &journalID
	balance.UpdatedAt = now
	m.balances[posting.AccountID] = balance
}

func copyOf[T any](v T) *T {
	return &v
}
