package corebanking

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestService(t *testing.T) (context.Context, *Service, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	svc := NewService(store, NewMockNIPProvider())
	ctx := context.Background()
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, svc, store
}

func createMemoryCustomerAccount(t *testing.T, svc *Service, ctx context.Context, firstName, lastName, email, accountNumber string) *Account {
	t.Helper()
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     firstName,
		LastName:      lastName,
		Email:         email,
		Phone:         "+2348012345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: accountNumber,
		Name:          firstName + " " + lastName + " Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

var testAccountNumberSequence uint64

func uniqueAccountNumber(prefix string) string {
	return fmt.Sprintf("%s%08d", prefix, atomic.AddUint64(&testAccountNumberSequence, 1))
}

func numberedTestUUID(prefix string, number int) string {
	return fmt.Sprintf("%s-%012d", prefix, number)
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

func externalOutbound(t *testing.T, svc *Service, ctx context.Context, input ExternalOutboundTransferInput) *ExternalOutboundTransferResult {
	t.Helper()
	result, err := svc.ExternalOutboundTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func externalOutboundTestInput(idempotencyKey string, amountMinor int64, scenario string) ExternalOutboundTransferInput {
	return ExternalOutboundTransferInput{
		InstitutionID:              DemoInstitutionID,
		SourceAccountID:            DemoCustomerAccountID,
		DestinationInstitutionCode: mockNIPDemoBankCode,
		DestinationAccountNumber:   mockNIPDemoAccountNumber,
		DestinationAccountName:     mockNIPDemoAccountName,
		AmountMinor:                amountMinor,
		CurrencyID:                 "NGN",
		IdempotencyKey:             idempotencyKey,
		Narration:                  "External outbound test",
		Reference:                  idempotencyKey + "-ref",
		Scenario:                   scenario,
	}
}

func externalRequery(t *testing.T, svc *Service, ctx context.Context, input ExternalTransferRequeryInput) *ExternalTransferRequeryResult {
	t.Helper()
	result, err := svc.ExternalTransferRequery(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func externalInbound(t *testing.T, svc *Service, ctx context.Context, payload map[string]any) *ExternalInboundEventResult {
	t.Helper()
	result, err := svc.ExternalInboundEvent(ctx, externalInboundInput(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func externalInboundInput(t *testing.T, payload map[string]any) ExternalInboundEventInput {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := payload["provider"].(string)
	return ExternalInboundEventInput{
		InstitutionID: DemoInstitutionID,
		Provider:      provider,
		Payload:       body,
		Headers:       map[string]string{"X-Institution-ID": DemoInstitutionID},
	}
}

func externalInboundPayload(eventID, reference, status string, amountMinor int64) map[string]any {
	return map[string]any{
		"provider_event_id":          eventID,
		"provider_reference":         reference,
		"destination_account_number": mockNIPDemoAccountNumber,
		"amount_minor":               amountMinor,
		"currency_id":                "NGN",
		"status":                     status,
		"sender_name":                "External Sender",
		"sender_account_number":      "1234567890",
		"sender_institution_code":    "999044",
		"narration":                  "External inbound test",
	}
}

func assertMemoryTransferHold(t *testing.T, store *memoryStore, transferID, status string) {
	t.Helper()
	hold, ok := store.holds[transferID]
	if !ok {
		t.Fatalf("missing hold for transfer %s", transferID)
	}
	if hold.Status != status {
		t.Fatalf("hold status mismatch: got %+v want status=%s", hold, status)
	}
}

func assertNoReconciliationItem(t *testing.T, items []ReconciliationItem, transferID string) {
	t.Helper()
	for _, item := range items {
		if item.TransferID == transferID {
			t.Fatalf("unexpected reconciliation item %s in %+v", transferID, items)
		}
	}
}

func reverseTransfer(t *testing.T, svc *Service, ctx context.Context, transferID, idempotencyKey string) *Transfer {
	t.Helper()
	transfer, err := svc.ReverseTransfer(ctx, DemoInstitutionID, transferID, idempotencyKey)
	return mustTransfer(t, transfer, err)
}

func mockProviderEvent(t *testing.T, svc *Service, ctx context.Context, event ProviderWebhookEvent) *Transfer {
	t.Helper()
	if event.Provider == "" {
		event.Provider = ProviderMockNIP
	}
	transfer, err := svc.recordProviderWebhookEvent(ctx, event)
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
	assertBalancePair(t, svc, ctx, institutionID, accountID, want, want)
}

func assertBalancePair(t *testing.T, svc *Service, ctx context.Context, institutionID, accountID string, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, institutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want available=%d ledger=%d", balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
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

func assertHistoryNewestFirst(t *testing.T, history []Transaction) {
	t.Helper()
	for i := 1; i < len(history); i++ {
		if history[i].CreatedAt.After(history[i-1].CreatedAt) {
			t.Fatalf("history is not ordered newest first at %d: %s after %s", i, history[i].CreatedAt, history[i-1].CreatedAt)
		}
		if history[i].CreatedAt.Equal(history[i-1].CreatedAt) && history[i].TransferID > history[i-1].TransferID {
			t.Fatalf("history tie-breaker is not stable at %d: %s after %s", i, history[i].TransferID, history[i-1].TransferID)
		}
	}
}

func assertTransfersNewestFirst(t *testing.T, transfers []Transfer) {
	t.Helper()
	for i := 1; i < len(transfers); i++ {
		if transfers[i].CreatedAt.After(transfers[i-1].CreatedAt) {
			t.Fatalf("transfers are not ordered newest first at %d: %s after %s", i, transfers[i].CreatedAt, transfers[i-1].CreatedAt)
		}
		if transfers[i].CreatedAt.Equal(transfers[i-1].CreatedAt) && transfers[i].ID > transfers[i-1].ID {
			t.Fatalf("transfer tie-breaker is not stable at %d: %s after %s", i, transfers[i].ID, transfers[i-1].ID)
		}
	}
}

func assertAuditEventsNewestFirst(t *testing.T, events []AuditEvent) {
	t.Helper()
	for i := 1; i < len(events); i++ {
		if events[i].CreatedAt.After(events[i-1].CreatedAt) {
			t.Fatalf("audit events are not ordered newest first at %d: %s after %s", i, events[i].CreatedAt, events[i-1].CreatedAt)
		}
		if events[i].CreatedAt.Equal(events[i-1].CreatedAt) && events[i].ID > events[i-1].ID {
			t.Fatalf("audit event tie-breaker is not stable at %d: %s after %s", i, events[i].ID, events[i-1].ID)
		}
	}
}

func assertNoDuplicateTransfers(t *testing.T, transfers []Transfer) {
	t.Helper()
	seen := map[string]bool{}
	for _, transfer := range transfers {
		if seen[transfer.ID] {
			t.Fatalf("transfer pagination returned duplicate %s", transfer.ID)
		}
		seen[transfer.ID] = true
	}
}

func assertNoDuplicateAuditEvents(t *testing.T, events []AuditEvent) {
	t.Helper()
	seen := map[string]bool{}
	for _, event := range events {
		if seen[event.ID] {
			t.Fatalf("audit pagination returned duplicate %s", event.ID)
		}
		seen[event.ID] = true
	}
}

func assertTransferListMissing(t *testing.T, transfers []Transfer, transferID string) {
	t.Helper()
	for _, transfer := range transfers {
		if transfer.ID == transferID {
			t.Fatalf("transfer %s unexpectedly present in list: %+v", transferID, transfers)
		}
	}
}

func assertAuditEventListMissing(t *testing.T, events []AuditEvent, eventID string) {
	t.Helper()
	for _, event := range events {
		if event.ID == eventID {
			t.Fatalf("audit event %s unexpectedly present in list: %+v", eventID, events)
		}
	}
}

func assertTransactionPresent(t *testing.T, history []Transaction, transferID string, signedMinor int64, direction string) {
	t.Helper()
	for _, txn := range history {
		if txn.TransferID != transferID {
			continue
		}
		if txn.SignedAmountMinor != signedMinor || txn.Direction != direction {
			t.Fatalf("transaction mismatch for transfer %s: got %+v want signed=%d direction=%s", transferID, txn, signedMinor, direction)
		}
		return
	}
	t.Fatalf("transfer %s not found in history: %+v", transferID, history)
}

func transactionDirectionFromSigned(signedMinor int64, fallback string) string {
	switch {
	case signedMinor > 0:
		return TransactionDirectionCredit
	case signedMinor < 0:
		return TransactionDirectionDebit
	case fallback == TransferDirectionInbound:
		return TransactionDirectionCredit
	default:
		return TransactionDirectionDebit
	}
}

func setTransferCreatedAt(t *testing.T, store *memoryStore, transferID string, createdAt time.Time) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	transfer := store.transfers[transferID]
	transfer.CreatedAt = createdAt
	store.transfers[transferID] = transfer
}

func putMemoryTransferForList(t *testing.T, store *memoryStore, transferID, institutionID string, createdAt time.Time) Transfer {
	t.Helper()
	transfer := Transfer{
		ID:                   transferID,
		InstitutionID:        institutionID,
		AccountID:            DemoCustomerAccountID,
		Direction:            TransferDirectionInbound,
		Status:               TransferStatusSucceeded,
		ProviderStatus:       TransferStatusSucceeded,
		LedgerStatus:         LedgerStatusPosted,
		ReconciliationStatus: ReconciliationStatusMatched,
		AmountMinor:          1000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "list-transfer-" + transferID,
		Provider:             ProviderLedgerInternal,
		ProviderReference:    "list-transfer-ref-" + transferID,
		Narration:            "List transfer pagination fixture",
		CreatedAt:            createdAt,
		UpdatedAt:            createdAt,
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.transfers[transfer.ID] = transfer
	return transfer
}

func putMemoryAuditEventForList(store *memoryStore, eventID, institutionID string, createdAt time.Time) AuditEvent {
	event := AuditEvent{
		ID:            eventID,
		InstitutionID: institutionID,
		ActorType:     "system",
		ActorID:       "system",
		RequestID:     "service",
		Action:        "audit.list_fixture",
		EntityType:    "transfer",
		EntityID:      eventID,
		Metadata:      map[string]string{},
		CreatedAt:     createdAt,
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.audits = append(store.audits, event)
	return event
}

func setMemoryAccountStatus(store *memoryStore, accountID, status string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	account := store.accounts[accountID]
	account.Status = status
	store.accounts[accountID] = account
}

func setMemoryAccountCurrency(store *memoryStore, accountID, currencyID string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	account := store.accounts[accountID]
	account.CurrencyID = currencyID
	store.accounts[accountID] = account
	balance := store.balances[accountID]
	balance.CurrencyID = currencyID
	store.balances[accountID] = balance
}

type memoryMoneyRows struct {
	balances  int
	transfers int
	journals  int
	postings  int
	holds     int
}

func memoryMoneyRowCounts(store *memoryStore) memoryMoneyRows {
	store.mu.Lock()
	defer store.mu.Unlock()

	postingCount := 0
	for _, postings := range store.postings {
		postingCount += len(postings)
	}
	return memoryMoneyRows{
		balances:  len(store.balances),
		transfers: len(store.transfers),
		journals:  len(store.journals),
		postings:  postingCount,
		holds:     len(store.holds),
	}
}

func assertNameEnquiryBalanceUnchanged(t *testing.T, before, after *AccountBalance) {
	t.Helper()
	if before.AccountID != after.AccountID ||
		before.InstitutionID != after.InstitutionID ||
		before.AvailableMinor != after.AvailableMinor ||
		before.LedgerMinor != after.LedgerMinor ||
		before.CurrencyID != after.CurrencyID ||
		optionalAuditValue(before.LastJournalEntryID) != optionalAuditValue(after.LastJournalEntryID) {
		t.Fatalf("name enquiry mutated balance: before=%+v after=%+v", before, after)
	}
}

type memoryStore struct {
	mu                        sync.Mutex
	institutions              map[string]Institution
	branches                  map[string]Branch
	customers                 map[string]Customer
	accounts                  map[string]Account
	balances                  map[string]AccountBalance
	transfers                 map[string]Transfer
	journals                  map[string]JournalEntry
	postings                  map[string][]Posting
	holds                     map[string]AccountHold
	audits                    []AuditEvent
	reconciliationReviews     map[string]memoryReconciliationReview
	idempotency               map[string]string
	providerEvents            map[string]string
	providerEventFingerprints map[string]string
}

type memoryReconciliationReview struct {
	status     string
	note       string
	reviewedAt time.Time
	reviewedBy string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		institutions:              map[string]Institution{},
		branches:                  map[string]Branch{},
		customers:                 map[string]Customer{},
		accounts:                  map[string]Account{},
		balances:                  map[string]AccountBalance{},
		transfers:                 map[string]Transfer{},
		journals:                  map[string]JournalEntry{},
		postings:                  map[string][]Posting{},
		holds:                     map[string]AccountHold{},
		audits:                    []AuditEvent{},
		reconciliationReviews:     map[string]memoryReconciliationReview{},
		idempotency:               map[string]string{},
		providerEvents:            map[string]string{},
		providerEventFingerprints: map[string]string{},
	}
}

func (m *memoryStore) EnsureDemoData(ctx context.Context) (*SeedResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	customerID := DemoCustomerID
	m.institutions[DemoInstitutionID] = Institution{ID: DemoInstitutionID, Name: "Lenz Demo Microfinance Bank", ShortName: "Lenz Demo", Code: "999001", CurrencyID: "NGN", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.branches[DemoBranchID] = Branch{ID: DemoBranchID, InstitutionID: DemoInstitutionID, Code: "HQ", Name: "Demo HQ", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.customers[DemoCustomerID] = Customer{ID: DemoCustomerID, InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: CustomerTypeIndividual, FirstName: "Ada", LastName: "Demo", Email: "ada.demo@example.com", Phone: "+2348000000000", Status: "active", KYCTier: CustomerKYCTier1, BVNStatus: CustomerIdentityStatusNotCollected, NINStatus: CustomerIdentityStatusNotCollected, CreatedAt: now, UpdatedAt: now}
	m.accounts[DemoCustomerAccountID] = Account{ID: DemoCustomerAccountID, InstitutionID: DemoInstitutionID, CustomerID: &customerID, AccountNumber: "9990000001", Name: "Ada Demo Wallet", Kind: AccountKindCustomer, ProductType: AccountProductStandardWallet, AllowNegative: false, CurrencyID: "NGN", NormalBalance: NormalBalanceCredit, Status: "active", CreatedAt: now, UpdatedAt: now}
	m.accounts[DemoClearingAccountID] = Account{ID: DemoClearingAccountID, InstitutionID: DemoInstitutionID, AccountNumber: "9999999999", Name: "Mock NIP Clearing", Kind: AccountKindInternal, ProductType: AccountProductInternal, AllowNegative: true, CurrencyID: "NGN", NormalBalance: NormalBalanceDebit, Status: "active", CreatedAt: now, UpdatedAt: now}
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

func (m *memoryStore) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	branch, ok := m.branches[input.BranchID]
	if !ok || branch.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	customer := Customer{
		ID:            uuid.Must(uuid.NewRandom()).String(),
		InstitutionID: input.InstitutionID,
		BranchID:      input.BranchID,
		CustomerType:  input.CustomerType,
		FirstName:     input.FirstName,
		LastName:      input.LastName,
		Email:         input.Email,
		Phone:         input.Phone,
		Status:        "active",
		KYCTier:       input.KYCTier,
		BVNStatus:     input.BVNStatus,
		NINStatus:     input.NINStatus,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if input.BusinessName != "" {
		customer.BusinessName = &input.BusinessName
	}
	m.customers[customer.ID] = customer
	if err := m.createAuditLocked(auditEventInput{
		InstitutionID: customer.InstitutionID,
		Action:        AuditActionCustomerCreated,
		EntityType:    "customer",
		EntityID:      customer.ID,
		CustomerID:    customer.ID,
		NewStatus:     customer.Status,
		Metadata:      map[string]string{"customer_type": customer.CustomerType, "branch_id": customer.BranchID},
		CreatedAt:     now,
	}); err != nil {
		delete(m.customers, customer.ID)
		return nil, err
	}
	return copyOf(customer), nil
}

func (m *memoryStore) GetCustomer(ctx context.Context, institutionID, customerID string) (*Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	customer, ok := m.customers[customerID]
	if !ok || customer.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(customer), nil
}

func (m *memoryStore) CreateAccount(ctx context.Context, input CreateAccountInput) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	customer, ok := m.customers[input.CustomerID]
	if !ok || customer.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	for _, account := range m.accounts {
		if account.InstitutionID == input.InstitutionID && account.AccountNumber == input.AccountNumber {
			return nil, ErrConflict
		}
	}
	now := time.Now().UTC()
	account := Account{
		ID:            uuid.Must(uuid.NewRandom()).String(),
		InstitutionID: input.InstitutionID,
		CustomerID:    &input.CustomerID,
		AccountNumber: input.AccountNumber,
		Name:          input.Name,
		Kind:          AccountKindCustomer,
		ProductType:   input.ProductType,
		AllowNegative: false,
		CurrencyID:    input.CurrencyID,
		NormalBalance: NormalBalanceCredit,
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m.accounts[account.ID] = account
	m.balances[account.ID] = AccountBalance{
		AccountID:      account.ID,
		InstitutionID:  account.InstitutionID,
		AvailableMinor: 0,
		LedgerMinor:    0,
		CurrencyID:     account.CurrencyID,
		UpdatedAt:      now,
	}
	if err := m.createAuditLocked(auditEventInput{
		InstitutionID: account.InstitutionID,
		Action:        AuditActionAccountCreated,
		EntityType:    "account",
		EntityID:      account.ID,
		CustomerID:    input.CustomerID,
		AccountID:     account.ID,
		NewStatus:     account.Status,
		Metadata:      map[string]string{"product_type": account.ProductType, "currency_id": account.CurrencyID},
		CreatedAt:     now,
	}); err != nil {
		delete(m.accounts, account.ID)
		delete(m.balances, account.ID)
		return nil, err
	}
	return copyOf(account), nil
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

func (m *memoryStore) GetAccountByNumber(ctx context.Context, institutionID, accountNumber string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, account := range m.accounts {
		if account.InstitutionID == institutionID && account.AccountNumber == accountNumber {
			return copyOf(account), nil
		}
	}
	return nil, ErrNotFound
}

func (m *memoryStore) GetDefaultInternalSettlementAccount(ctx context.Context, institutionID, currencyID string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var defaultAccount *Account
	for _, account := range m.accounts {
		if !validInternalSettlementAccount(account, institutionID, currencyID) {
			continue
		}
		if defaultAccount != nil {
			return nil, ErrInvalidRequest
		}
		accountCopy := account
		defaultAccount = &accountCopy
	}
	if defaultAccount == nil {
		return nil, ErrNotFound
	}
	return defaultAccount, nil
}

func (m *memoryStore) SetAccountStatus(ctx context.Context, input AccountControlInput, status string, allowedCurrentStatuses ...string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	account, ok := m.accounts[input.AccountID]
	if !ok || account.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	oldStatus := account.Status
	if !allowedAccountStatusTransition(oldStatus, allowedCurrentStatuses) {
		return nil, ErrInvalidRequest
	}
	if oldStatus == status {
		return copyOf(account), nil
	}
	account.Status = status
	account.UpdatedAt = time.Now().UTC()
	if err := m.createAuditLocked(auditEventInput{
		InstitutionID: input.InstitutionID,
		Action:        accountStatusAuditAction(oldStatus, status),
		EntityType:    "account",
		EntityID:      account.ID,
		CustomerID:    optionalAuditValue(account.CustomerID),
		AccountID:     account.ID,
		Reference:     input.Reference,
		OldStatus:     oldStatus,
		NewStatus:     status,
		Metadata:      map[string]string{"reason": input.Reason},
		CreatedAt:     account.UpdatedAt,
	}); err != nil {
		return nil, err
	}
	m.accounts[input.AccountID] = account
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

func (m *memoryStore) PlaceAccountLien(ctx context.Context, input AccountLienInput) (*AccountHold, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	account, ok := m.accounts[input.AccountID]
	if !ok || account.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	balance, ok := m.balances[input.AccountID]
	if !ok || balance.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if account.Kind != AccountKindCustomer || account.Status == AccountStatusClosed {
		return nil, ErrInvalidRequest
	}
	for _, hold := range m.holds {
		if hold.InstitutionID == input.InstitutionID && hold.AccountID == input.AccountID && hold.TransferID == nil && hold.Status == HoldStatusActive && hold.Reference == input.Reference {
			if !accountLienReplayMatches(input, &hold) {
				return nil, ErrConflict
			}
			return copyOf(hold), nil
		}
	}
	for _, hold := range m.holds {
		if hold.InstitutionID == input.InstitutionID && hold.AccountID != input.AccountID && hold.TransferID == nil && hold.Status == HoldStatusActive && hold.Reference == input.Reference {
			return nil, ErrConflict
		}
	}
	if account.CurrencyID != input.CurrencyID {
		return nil, ErrInvalidRequest
	}
	if balance.AvailableMinor < input.AmountMinor {
		return nil, ErrInsufficient
	}
	now := time.Now().UTC()
	hold := AccountHold{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, AccountID: input.AccountID, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, Status: HoldStatusActive, Reason: input.Reason, Reference: input.Reference, CreatedAt: now, UpdatedAt: now}
	m.holds[hold.ID] = hold
	balance.AvailableMinor -= input.AmountMinor
	balance.UpdatedAt = now
	m.balances[input.AccountID] = balance
	if err := m.createAuditLocked(auditEventInput{
		InstitutionID: input.InstitutionID,
		Action:        AuditActionLienPlaced,
		EntityType:    "account_hold",
		EntityID:      hold.ID,
		AccountID:     hold.AccountID,
		Reference:     input.Reference,
		NewStatus:     HoldStatusActive,
		Metadata:      map[string]string{"amount_minor": formatAuditInt(input.AmountMinor), "currency_id": input.CurrencyID, "reason": input.Reason},
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}
	return copyOf(hold), nil
}

func (m *memoryStore) ReleaseAccountLien(ctx context.Context, input ReleaseLienInput) (*AccountHold, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hold, ok := m.holds[input.LienID]
	if !ok || hold.InstitutionID != input.InstitutionID || hold.AccountID != input.AccountID || hold.TransferID != nil {
		return nil, ErrNotFound
	}
	if hold.Status != HoldStatusActive {
		return copyOf(hold), nil
	}
	now := time.Now().UTC()
	hold.Status = HoldStatusReleased
	hold.UpdatedAt = now
	hold.ReleasedAt = &now
	m.holds[input.LienID] = hold
	balance := m.balances[input.AccountID]
	balance.AvailableMinor += hold.AmountMinor
	balance.UpdatedAt = now
	m.balances[input.AccountID] = balance
	if err := m.createAuditLocked(auditEventInput{
		InstitutionID: input.InstitutionID,
		Action:        AuditActionLienReleased,
		EntityType:    "account_hold",
		EntityID:      hold.ID,
		AccountID:     hold.AccountID,
		Reference:     input.Reference,
		OldStatus:     HoldStatusActive,
		NewStatus:     HoldStatusReleased,
		Metadata:      map[string]string{"amount_minor": formatAuditInt(hold.AmountMinor), "currency_id": hold.CurrencyID, "reason": input.Reason},
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}
	return copyOf(hold), nil
}

func (m *memoryStore) createAuditLocked(input auditEventInput) error {
	return m.createAuditLockedContext(context.Background(), input)
}

func (m *memoryStore) createAuditLockedContext(ctx context.Context, input auditEventInput) error {
	event, _, err := newAuditEvent(ctx, input)
	if err != nil {
		return err
	}
	m.audits = append(m.audits, event)
	return nil
}

func (m *memoryStore) ListAuditEvents(ctx context.Context, institutionID string, options ListAuditEventsOptions) ([]AuditEvent, error) {
	options, err := normalizeListAuditEventsOptions(options)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	events := []AuditEvent{}
	for _, event := range m.audits {
		if event.InstitutionID == institutionID && auditEventBeforeCursor(event, options) {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID > events[j].ID
		}
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	if len(events) > options.Limit {
		events = events[:options.Limit]
	}
	return events, nil
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

func (m *memoryStore) GetTransferByIdempotency(ctx context.Context, institutionID, idempotencyKey string) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.idempotency[strings.TrimSpace(institutionID)+"|"+strings.TrimSpace(idempotencyKey)]
	if id == "" {
		return nil, ErrNotFound
	}
	return copyOf(m.transfers[id]), nil
}

func (m *memoryStore) ListTransfers(ctx context.Context, institutionID string, options ListTransfersOptions) ([]Transfer, error) {
	options, err := normalizeListTransfersOptions(options)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var transfers []Transfer
	for _, transfer := range m.transfers {
		if transfer.InstitutionID == institutionID && transferBeforeCursor(transfer, options) {
			transfers = append(transfers, transfer)
		}
	}
	sort.Slice(transfers, func(i, j int) bool {
		if transfers[i].CreatedAt.Equal(transfers[j].CreatedAt) {
			return transfers[i].ID > transfers[j].ID
		}
		return transfers[i].CreatedAt.After(transfers[j].CreatedAt)
	})
	if len(transfers) > options.Limit {
		transfers = transfers[:options.Limit]
	}
	return transfers, nil
}

func (m *memoryStore) ListReconciliationItems(ctx context.Context, institutionID string, options ListReconciliationItemsOptions) ([]ReconciliationItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := reconciliationStalePendingCutoff(options)
	items := []ReconciliationItem{}
	for _, transfer := range m.transfers {
		if transfer.InstitutionID != institutionID {
			continue
		}
		item := m.reconciliationItemFromTransferLocked(transfer)
		decorateReconciliationItem(&item, cutoff)
		if !reconciliationItemBelongsInQueue(item, cutoff) || !reconciliationItemMatchesFilters(item, options) || !reconciliationItemBeforeCursor(item, options) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].TransferID > items[j].TransferID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > options.Limit {
		items = items[:options.Limit]
	}
	return items, nil
}

func (m *memoryStore) GetReconciliationItem(ctx context.Context, institutionID, transferID string) (*ReconciliationItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[transferID]
	if !ok || transfer.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	cutoff := reconciliationStalePendingCutoff(ListReconciliationItemsOptions{})
	item := m.reconciliationItemFromTransferLocked(transfer)
	decorateReconciliationItem(&item, cutoff)
	if !isInspectableReconciliationItem(item, cutoff) {
		return nil, ErrNotFound
	}
	return copyOf(item), nil
}

func (m *memoryStore) MarkReconciliationItemReviewed(ctx context.Context, input MarkReconciliationItemReviewedInput) (*ReconciliationItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[input.TransferID]
	if !ok || transfer.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	cutoff := reconciliationStalePendingCutoff(ListReconciliationItemsOptions{})
	item := m.reconciliationItemFromTransferLocked(transfer)
	decorateReconciliationItem(&item, cutoff)
	if !isInspectableReconciliationItem(item, cutoff) {
		return nil, ErrNotFound
	}
	oldReviewStatus := optionalAuditValue(item.ReviewStatus)
	_, actorID, _, _ := auditContext(ctx)
	now := time.Now().UTC()
	m.reconciliationReviews[input.TransferID] = memoryReconciliationReview{
		status:     input.ResolutionStatus,
		note:       input.ResolutionNote,
		reviewedAt: now,
		reviewedBy: actorID,
	}
	transfer.UpdatedAt = now
	m.transfers[input.TransferID] = transfer
	if err := m.createAuditLockedContext(ctx, auditEventInput{
		InstitutionID:  transfer.InstitutionID,
		Action:         AuditActionReconciliationReviewed,
		EntityType:     "transfer",
		EntityID:       transfer.ID,
		AccountID:      transfer.AccountID,
		TransferID:     transfer.ID,
		JournalEntryID: optionalAuditValue(transfer.JournalEntryID),
		Reference:      transfer.ProviderReference,
		OldStatus:      oldReviewStatus,
		NewStatus:      input.ResolutionStatus,
		Metadata: map[string]string{
			"resolution_note":       input.ResolutionNote,
			"review_reason":         item.ReviewReason,
			"recommended_action":    item.RecommendedNextAction,
			"provider_status":       item.ProviderStatus,
			"ledger_status":         item.LedgerStatus,
			"reconciliation_status": item.ReconciliationStatus,
		},
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	updated := m.reconciliationItemFromTransferLocked(transfer)
	decorateReconciliationItem(&updated, cutoff)
	return copyOf(updated), nil
}

func (m *memoryStore) reconciliationItemFromTransferLocked(transfer Transfer) ReconciliationItem {
	item := ReconciliationItem{
		TransferID:           transfer.ID,
		InstitutionID:        transfer.InstitutionID,
		AccountID:            transfer.AccountID,
		Direction:            transfer.Direction,
		Status:               transfer.Status,
		Provider:             transfer.Provider,
		ProviderReference:    transfer.ProviderReference,
		ProviderEventID:      transfer.ProviderEventID,
		ProviderStatus:       transfer.ProviderStatus,
		LedgerStatus:         transfer.LedgerStatus,
		ReconciliationStatus: transfer.ReconciliationStatus,
		AmountMinor:          transfer.AmountMinor,
		CurrencyID:           transfer.CurrencyID,
		FailureReason:        transfer.FailureReason,
		JournalEntryID:       transfer.JournalEntryID,
		CreatedAt:            transfer.CreatedAt,
		UpdatedAt:            transfer.UpdatedAt,
	}
	if review, ok := m.reconciliationReviews[transfer.ID]; ok {
		item.ReviewStatus = &review.status
		item.ReviewNote = &review.note
		item.ReviewedAt = &review.reviewedAt
		item.ReviewedBy = &review.reviewedBy
	}
	return item
}

func reconciliationItemBelongsInQueue(item ReconciliationItem, cutoff time.Time) bool {
	if item.ReviewStatus != nil && *item.ReviewStatus != ReconciliationReviewStatusManualFollowupRequired {
		return false
	}
	_, _, needsReview := reconciliationReviewState(item, cutoff)
	return needsReview
}

func reconciliationItemMatchesFilters(item ReconciliationItem, options ListReconciliationItemsOptions) bool {
	if options.Status != "" && item.Status != options.Status {
		return false
	}
	if options.ProviderStatus != "" && item.ProviderStatus != options.ProviderStatus {
		return false
	}
	if options.LedgerStatus != "" && item.LedgerStatus != options.LedgerStatus {
		return false
	}
	if options.ReconciliationStatus != "" && item.ReconciliationStatus != options.ReconciliationStatus {
		return false
	}
	return true
}

func reconciliationItemBeforeCursor(item ReconciliationItem, options ListReconciliationItemsOptions) bool {
	return createdBeforeCursor(item.CreatedAt, item.TransferID, options.BeforeCreatedAt, options.BeforeTransferID)
}

func transferBeforeCursor(transfer Transfer, options ListTransfersOptions) bool {
	return createdBeforeCursor(transfer.CreatedAt, transfer.ID, options.BeforeCreatedAt, options.BeforeTransferID)
}

func auditEventBeforeCursor(event AuditEvent, options ListAuditEventsOptions) bool {
	return createdBeforeCursor(event.CreatedAt, event.ID, options.BeforeCreatedAt, options.BeforeAuditEventID)
}

func createdBeforeCursor(createdAt time.Time, id string, beforeCreatedAt *time.Time, beforeID string) bool {
	if beforeCreatedAt == nil {
		return true
	}
	if beforeID != "" {
		return createdAt.Before(*beforeCreatedAt) || (createdAt.Equal(*beforeCreatedAt) && id < beforeID)
	}
	return createdAt.Before(*beforeCreatedAt)
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

func (m *memoryStore) ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	options = normalizeListTransactionsOptions(options)
	txns := []Transaction{}
	for _, transfer := range m.transfers {
		if transfer.InstitutionID != institutionID {
			continue
		}
		if options.BeforeCreatedAt != nil {
			if options.BeforeTransferID != "" {
				if transfer.CreatedAt.After(*options.BeforeCreatedAt) || (transfer.CreatedAt.Equal(*options.BeforeCreatedAt) && transfer.ID >= options.BeforeTransferID) {
					continue
				}
			} else if !transfer.CreatedAt.Before(*options.BeforeCreatedAt) {
				continue
			}
		}
		signed := int64(0)
		direction := transactionDirectionFromSigned(0, transfer.Direction)
		postedForAccount := false
		var counterpartyAccountID *string
		if transfer.Status == TransferStatusSucceeded && transfer.JournalEntryID != nil {
			for _, posting := range m.postings[*transfer.JournalEntryID] {
				if posting.AccountID == accountID {
					account := m.accounts[accountID]
					if (account.NormalBalance == NormalBalanceCredit && posting.Direction == PostingCredit) || (account.NormalBalance == NormalBalanceDebit && posting.Direction == PostingDebit) {
						signed = posting.AmountMinor
					} else {
						signed = -posting.AmountMinor
					}
					direction = transactionDirectionFromSigned(signed, transfer.Direction)
					postedForAccount = true
				} else {
					if m.accounts[posting.AccountID].Kind == AccountKindCustomer {
						counterparty := posting.AccountID
						counterpartyAccountID = &counterparty
					}
				}
			}
		}
		if transfer.AccountID != accountID && !postedForAccount {
			continue
		}
		txns = append(txns, Transaction{
			ID:                    transfer.ID,
			TransferID:            transfer.ID,
			JournalEntryID:        transfer.JournalEntryID,
			AccountID:             accountID,
			InstitutionID:         transfer.InstitutionID,
			Direction:             direction,
			Status:                transfer.Status,
			LedgerStatus:          transfer.LedgerStatus,
			ProviderStatus:        transfer.ProviderStatus,
			ReconciliationStatus:  transfer.ReconciliationStatus,
			AmountMinor:           transfer.AmountMinor,
			SignedAmountMinor:     signed,
			CurrencyID:            transfer.CurrencyID,
			Narration:             transfer.Narration,
			CounterpartyAccountID: counterpartyAccountID,
			Provider:              transfer.Provider,
			ProviderReference:     transfer.ProviderReference,
			CreatedAt:             transfer.CreatedAt,
		})
	}
	sort.Slice(txns, func(i, j int) bool {
		if txns[i].CreatedAt.Equal(txns[j].CreatedAt) {
			return txns[i].TransferID > txns[j].TransferID
		}
		return txns[i].CreatedAt.After(txns[j].CreatedAt)
	})
	if len(txns) > options.Limit {
		txns = txns[:options.Limit]
	}
	return txns, nil
}

func (m *memoryStore) RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recordTransferLocked(input)
}

func (m *memoryStore) RecordProviderEventReview(ctx context.Context, input RecordProviderEventReviewInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalized, err := normalizeProviderEventReviewInput(input)
	if err != nil {
		return nil, err
	}
	input = normalized

	if input.ReserveProviderEvent {
		key := input.InstitutionID + "|" + input.Provider + "|" + input.ProviderEventID
		if id := m.providerEvents[key]; id != "" {
			if !m.providerEventPayloadMatchesLocked(key, input.RequestFingerprint) {
				return nil, ErrConflict
			}
			return copyOf(m.transfers[id]), nil
		}
	}
	if id := m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey]; id != "" {
		transfer := m.transfers[id]
		if !transferRequestFingerprintMatches(&transfer, input.RequestFingerprint) {
			return nil, ErrConflict
		}
		return copyOf(transfer), nil
	}

	account, ok := m.accounts[input.AccountID]
	if !ok || account.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	transfer := Transfer{
		ID:                   uuid.Must(uuid.NewRandom()).String(),
		InstitutionID:        input.InstitutionID,
		AccountID:            input.AccountID,
		Direction:            input.Direction,
		Status:               input.Status,
		ProviderStatus:       input.ProviderStatus,
		LedgerStatus:         LedgerStatusNoPosting,
		ReconciliationStatus: ReconciliationStatusManualReview,
		AmountMinor:          input.AmountMinor,
		CurrencyID:           input.CurrencyID,
		IdempotencyKey:       input.IdempotencyKey,
		Provider:             input.Provider,
		ProviderReference:    input.ProviderReference,
		ProviderEventID:      &input.ProviderEventID,
		RequestFingerprint:   input.RequestFingerprint,
		Narration:            input.Narration,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if input.FailureReason != "" {
		transfer.FailureReason = &input.FailureReason
	}
	m.transfers[transfer.ID] = transfer
	m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey] = transfer.ID
	if input.ReserveProviderEvent {
		key := input.InstitutionID + "|" + input.Provider + "|" + input.ProviderEventID
		m.providerEvents[key] = transfer.ID
		m.providerEventFingerprints[key] = input.RequestFingerprint
	}
	auditInput, ok := externalInboundTransferAuditInput(transfer, account, input.FailureReason)
	if ok {
		auditInput.Action = AuditActionExternalInboundReview
		if input.FailureReason != "" {
			auditInput.Metadata["review_reason"] = input.FailureReason
		}
		if err := m.createAuditLockedContext(ctx, auditInput); err != nil {
			return nil, err
		}
	}
	return copyOf(transfer), nil
}

func (m *memoryStore) BeginExternalOutboundTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	requestFingerprint := transferRequestFingerprint(input)
	if id := m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey]; id != "" {
		transfer := m.transfers[id]
		if !m.sameTransferReplayLocked(transfer, input, requestFingerprint) {
			return nil, false, ErrConflict
		}
		return copyOf(transfer), false, nil
	}
	transfer, err := m.recordTransferLocked(input)
	return transfer, err == nil, err
}

func (m *memoryStore) CompleteExternalOutboundTransfer(ctx context.Context, transferID string, input RecordTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completeExternalOutboundTransferLocked(ctx, transferID, input)
}

func (m *memoryStore) CompleteExternalTransferRequery(ctx context.Context, transferID string, input RecordTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending, ok := m.transfers[transferID]
	if !ok || pending.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if pending.Provider == ProviderLedgerInternal {
		return nil, ErrInvalidRequest
	}
	if pending.Provider != input.Provider ||
		pending.Direction != input.Direction ||
		pending.AccountID != input.AccountID ||
		pending.AmountMinor != input.AmountMinor ||
		pending.CurrencyID != input.CurrencyID {
		return nil, ErrConflict
	}
	if input.ProviderReference == "" {
		input.ProviderReference = pending.ProviderReference
	}
	if input.ProviderReference == "" {
		return nil, ErrInvalidRequest
	}
	if pending.ProviderReference != "" && pending.ProviderReference != input.ProviderReference {
		return nil, ErrConflict
	}
	if pending.Status != TransferStatusPending {
		return copyOf(pending), nil
	}
	if pending.LedgerStatus != LedgerStatusPending || !requeryableProviderStatus(pending.ProviderStatus) {
		return nil, ErrInvalidRequest
	}
	if pending.Direction == TransferDirectionOutbound {
		return m.completeExternalOutboundTransferLocked(ctx, transferID, input)
	}
	status, providerStatus, err := externalOutboundTransferStatuses(input)
	if err != nil {
		return nil, err
	}
	if status != TransferStatusPending {
		return m.settlePendingTransferLocked(pending, input)
	}

	account := m.accounts[pending.AccountID]
	now := time.Now().UTC()
	ledgerStatus, reconciliationStatus := transferStatuses(TransferStatusPending)
	failureReason := strings.TrimSpace(input.FailureReason)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
		if failureReason == "" {
			failureReason = providerUnknownFailureReason
		}
	}
	pending.Status = TransferStatusPending
	pending.ProviderStatus = providerStatus
	pending.LedgerStatus = ledgerStatus
	pending.ReconciliationStatus = reconciliationStatus
	pending.ProviderReference = input.ProviderReference
	pending.Narration = firstNonBlank(input.Narration, pending.Narration)
	pending.UpdatedAt = now
	if failureReason != "" {
		pending.FailureReason = &failureReason
	}
	m.transfers[pending.ID] = pending
	input.FailureReason = failureReason
	if err := m.auditExternalInboundTransferLocked(input, pending, account); err != nil {
		return nil, err
	}
	return copyOf(pending), nil
}

func (m *memoryStore) completeExternalOutboundTransferLocked(ctx context.Context, transferID string, input RecordTransferInput) (*Transfer, error) {
	pending, ok := m.transfers[transferID]
	if !ok || pending.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if pending.Direction != TransferDirectionOutbound ||
		pending.AccountID != input.AccountID ||
		pending.AmountMinor != input.AmountMinor ||
		pending.CurrencyID != input.CurrencyID ||
		pending.Provider != input.Provider {
		return nil, ErrConflict
	}
	if input.RequestFingerprint != "" && pending.RequestFingerprint != "" && input.RequestFingerprint != pending.RequestFingerprint {
		return nil, ErrConflict
	}
	if pending.Status != TransferStatusPending {
		return copyOf(pending), nil
	}

	account := m.accounts[pending.AccountID]
	clearing, ok := m.accounts[input.ClearingAccountID]
	if !ok || clearing.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	status, providerStatus, err := externalOutboundTransferStatuses(input)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	failureReason := strings.TrimSpace(input.FailureReason)
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	switch status {
	case TransferStatusSucceeded:
		if err := enforceTransferControls(input, account, clearing); err != nil {
			return nil, err
		}
		hold, ok := m.holds[pending.ID]
		if !ok || hold.Status != HoldStatusActive {
			return nil, ErrConflict
		}
		journalID := m.postJournalLocked(input, pending.ID, now, pending.AccountID)
		pending.JournalEntryID = &journalID
		m.consumeHoldLocked(pending.ID, now)
	case TransferStatusFailed:
		m.releaseHoldLocked(pending.ID, now)
	case TransferStatusPending:
		hold, ok := m.holds[pending.ID]
		if !ok || hold.Status != HoldStatusActive {
			return nil, ErrConflict
		}
	default:
		return nil, ErrInvalidRequest
	}
	pending.Status = status
	pending.ProviderStatus = providerStatus
	pending.LedgerStatus = ledgerStatus
	pending.ReconciliationStatus = reconciliationStatus
	pending.ProviderReference = firstNonBlank(input.ProviderReference, pending.ProviderReference)
	pending.Narration = firstNonBlank(input.Narration, pending.Narration)
	pending.UpdatedAt = now
	if input.ProviderEventID != "" {
		pending.ProviderEventID = &input.ProviderEventID
	}
	if failureReason != "" {
		pending.FailureReason = &failureReason
	}
	m.transfers[pending.ID] = pending
	if auditInput, ok := externalOutboundTransferAuditInput(input, pending, account, clearing); ok {
		if err := m.createAuditLockedContext(ctx, auditInput); err != nil {
			return nil, err
		}
	}
	return copyOf(pending), nil
}

func (m *memoryStore) GetTransferHold(ctx context.Context, institutionID, transferID string) (*AccountHold, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hold, ok := m.holds[transferID]
	if !ok || hold.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(hold), nil
}

func (m *memoryStore) ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	input.InstitutionID = strings.TrimSpace(input.InstitutionID)
	input.TransferID = strings.TrimSpace(input.TransferID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.InstitutionID == "" || input.TransferID == "" || input.IdempotencyKey == "" {
		return nil, ErrInvalidRequest
	}
	original, ok := m.transfers[input.TransferID]
	if !ok || original.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if original.Status != TransferStatusSucceeded || original.JournalEntryID == nil || original.Direction == TransferDirectionReversal {
		return nil, ErrInvalidRequest
	}
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = original.Provider
	}
	providerReference := strings.TrimSpace(input.ProviderReference)
	if providerReference == "" {
		originalReference := strings.TrimSpace(original.ProviderReference)
		if originalReference == "" {
			originalReference = original.ID
		}
		providerReference = "reversal:" + originalReference
	}
	narration := strings.TrimSpace(input.Narration)
	if narration == "" {
		narration = "Reversal of " + original.ID
	}
	direction := TransferDirectionOutbound
	if original.Direction == TransferDirectionOutbound {
		direction = TransferDirectionInbound
	}
	counterpartyAccountID, err := m.originalCounterpartyAccountIDLocked(original)
	if err != nil {
		return nil, err
	}
	reversal, err := m.recordTransferLocked(RecordTransferInput{InstitutionID: input.InstitutionID, AccountID: original.AccountID, ClearingAccountID: counterpartyAccountID, Direction: direction, Status: TransferStatusSucceeded, AmountMinor: original.AmountMinor, CurrencyID: original.CurrencyID, IdempotencyKey: input.IdempotencyKey, Provider: provider, ProviderReference: providerReference, ProviderEventID: strings.TrimSpace(input.ProviderEventID), ReversalOfTransferID: original.ID, FailureReason: strings.TrimSpace(input.FailureReason), Narration: narration})
	if err != nil {
		return nil, err
	}
	if reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != original.ID {
		return nil, ErrConflict
	}
	reversal.Direction = TransferDirectionReversal
	m.transfers[reversal.ID] = *reversal
	return reversal, nil
}

func (m *memoryStore) originalCounterpartyAccountIDLocked(original Transfer) (string, error) {
	if original.JournalEntryID == nil {
		return "", ErrInvalidRequest
	}
	postings := m.postings[*original.JournalEntryID]
	counterpartyAccountID := ""
	for _, posting := range postings {
		if posting.AccountID == original.AccountID {
			continue
		}
		if counterpartyAccountID != "" && posting.AccountID != counterpartyAccountID {
			return "", ErrDataIntegrity
		}
		counterpartyAccountID = posting.AccountID
	}
	if counterpartyAccountID == "" {
		return "", ErrDataIntegrity
	}
	return counterpartyAccountID, nil
}

func (m *memoryStore) recordTransferLocked(input RecordTransferInput) (*Transfer, error) {
	requestFingerprint := transferRequestFingerprint(input)
	if id := m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey]; id != "" {
		transfer := m.transfers[id]
		if !m.sameTransferReplayLocked(transfer, input, requestFingerprint) {
			return nil, ErrConflict
		}
		return copyOf(m.transfers[id]), nil
	}
	if input.ProviderEventID != "" {
		key := input.InstitutionID + "|" + input.Provider + "|" + input.ProviderEventID
		if id := m.providerEvents[key]; id != "" {
			if !m.providerEventPayloadMatchesLocked(key, requestFingerprint) {
				return nil, ErrConflict
			}
			return copyOf(m.transfers[id]), nil
		}
	}
	if input.ProviderReference != "" && input.Status != TransferStatusPending {
		for _, transfer := range m.transfers {
			if transfer.InstitutionID == input.InstitutionID && transfer.Provider == input.Provider && transfer.ProviderReference == input.ProviderReference && transfer.Direction == input.Direction && transfer.Status == TransferStatusPending {
				return m.settlePendingTransferLocked(transfer, input)
			}
		}
	}
	account, ok := m.accounts[input.AccountID]
	if !ok || account.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	clearing, ok := m.accounts[input.ClearingAccountID]
	if !ok || clearing.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if err := enforceTransferControls(input, account, clearing); err != nil {
		return nil, err
	}
	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := input.Status
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	failureReason := input.FailureReason
	balance := m.balances[input.AccountID]
	insufficient := customerInitiatedOutbound(input) && !canUseAvailableBalance(account, balance.AvailableMinor, input.AmountMinor)
	if customerInitiatedOutbound(input) && input.RequireAvailable && balance.AvailableMinor < input.AmountMinor {
		insufficient = true
	}
	if insufficient {
		if input.RejectInsufficient {
			return nil, ErrInsufficient
		}
		status = TransferStatusFailed
		failureReason = "insufficient_funds"
	}
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	if status == TransferStatusSucceeded && wouldCreateReversalDeficit(account, balance, input) {
		ledgerStatus = LedgerStatusReversalDeficit
		reconciliationStatus = ReconciliationStatusManualReview
	}
	now := time.Now().UTC()
	transfer := Transfer{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, AccountID: input.AccountID, Direction: input.Direction, Status: status, ProviderStatus: providerStatus, LedgerStatus: ledgerStatus, ReconciliationStatus: reconciliationStatus, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, IdempotencyKey: input.IdempotencyKey, Provider: input.Provider, ProviderReference: input.ProviderReference, RequestFingerprint: requestFingerprint, Narration: input.Narration, CreatedAt: now, UpdatedAt: now}
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
		journalID := m.postJournalLocked(input, transfer.ID, now, "")
		transfer.JournalEntryID = &journalID
	}
	m.transfers[transfer.ID] = transfer
	if status == TransferStatusPending && input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == "" {
		m.createHoldLocked(transfer, now)
	}
	m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey] = transfer.ID
	if input.ProviderEventID != "" {
		key := input.InstitutionID + "|" + input.Provider + "|" + input.ProviderEventID
		m.providerEvents[key] = transfer.ID
		m.providerEventFingerprints[key] = requestFingerprint
	}
	if err := m.auditPostedInternalTransferLocked(input, transfer, account, clearing); err != nil {
		return nil, err
	}
	if err := m.auditExternalInboundTransferLocked(input, transfer, account); err != nil {
		return nil, err
	}
	return copyOf(transfer), nil
}

func (m *memoryStore) providerEventPayloadMatchesLocked(key, requestFingerprint string) bool {
	existingFingerprint := strings.TrimSpace(m.providerEventFingerprints[key])
	return existingFingerprint == "" || existingFingerprint == strings.TrimSpace(requestFingerprint)
}

func (m *memoryStore) sameTransferReplayLocked(transfer Transfer, input RecordTransferInput, requestFingerprint string) bool {
	if transferRequestFingerprintMatches(&transfer, requestFingerprint) {
		return true
	}
	if strings.TrimSpace(transfer.RequestFingerprint) != "" || !sameTransferReplayFields(&transfer, input) || transfer.JournalEntryID == nil {
		return false
	}
	counterpartyAccountID, err := m.originalCounterpartyAccountIDLocked(transfer)
	return err == nil && counterpartyAccountID == input.ClearingAccountID
}

func (m *memoryStore) settlePendingTransferLocked(pending Transfer, input RecordTransferInput) (*Transfer, error) {
	if pending.AccountID != input.AccountID || pending.AmountMinor != input.AmountMinor || pending.CurrencyID != input.CurrencyID {
		return nil, ErrConflict
	}
	account := m.accounts[pending.AccountID]
	balance := m.balances[pending.AccountID]
	var clearing Account
	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := input.Status
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	failureReason := input.FailureReason
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	now := time.Now().UTC()
	if status == TransferStatusSucceeded && wouldCreateReversalDeficit(account, balance, input) {
		ledgerStatus = LedgerStatusReversalDeficit
		reconciliationStatus = ReconciliationStatusManualReview
	}
	switch status {
	case TransferStatusSucceeded:
		var ok bool
		clearing, ok = m.accounts[input.ClearingAccountID]
		if !ok || clearing.InstitutionID != input.InstitutionID {
			return nil, ErrNotFound
		}
		if err := enforceTransferControls(input, account, clearing); err != nil {
			return nil, err
		}
		heldAccountID := ""
		if pending.Direction == TransferDirectionOutbound && pending.ReversalOfTransferID == nil {
			if hold, ok := m.holds[pending.ID]; !ok || hold.Status != HoldStatusActive {
				return nil, ErrConflict
			}
			heldAccountID = pending.AccountID
		}
		journalID := m.postJournalLocked(input, pending.ID, now, heldAccountID)
		pending.JournalEntryID = &journalID
		if heldAccountID != "" {
			m.consumeHoldLocked(pending.ID, now)
		}
	case TransferStatusFailed:
		if pending.Direction == TransferDirectionOutbound && pending.ReversalOfTransferID == nil {
			m.releaseHoldLocked(pending.ID, now)
		}
	default:
		return nil, ErrInvalidRequest
	}
	pending.Status = status
	pending.ProviderStatus = providerStatus
	pending.LedgerStatus = ledgerStatus
	pending.ReconciliationStatus = reconciliationStatus
	pending.UpdatedAt = now
	if input.ProviderEventID != "" {
		pending.ProviderEventID = &input.ProviderEventID
		key := input.InstitutionID + "|" + input.Provider + "|" + input.ProviderEventID
		requestFingerprint := transferRequestFingerprint(input)
		if !m.providerEventPayloadMatchesLocked(key, requestFingerprint) {
			return nil, ErrConflict
		}
		m.providerEvents[key] = pending.ID
		m.providerEventFingerprints[key] = requestFingerprint
	}
	if failureReason != "" {
		pending.FailureReason = &failureReason
	}
	if strings.TrimSpace(input.Narration) != "" {
		pending.Narration = input.Narration
	}
	m.transfers[pending.ID] = pending
	if err := m.auditPostedInternalTransferLocked(input, pending, account, clearing); err != nil {
		return nil, err
	}
	if err := m.auditExternalInboundTransferLocked(input, pending, account); err != nil {
		return nil, err
	}
	return copyOf(pending), nil
}

func (m *memoryStore) auditPostedInternalTransferLocked(input RecordTransferInput, transfer Transfer, account, clearing Account) error {
	auditInput, ok := postedInternalTransferAuditInput(input, transfer, account, clearing)
	if !ok {
		return nil
	}
	return m.createAuditLocked(auditInput)
}

func (m *memoryStore) auditExternalInboundTransferLocked(input RecordTransferInput, transfer Transfer, account Account) error {
	auditInput, ok := externalInboundTransferAuditInput(transfer, account, input.FailureReason)
	if !ok {
		return nil
	}
	return m.createAuditLocked(auditInput)
}

func (m *memoryStore) postJournalLocked(input RecordTransferInput, transferID string, now time.Time, heldAccountID string) string {
	journalID := uuid.Must(uuid.NewRandom()).String()
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
		{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, JournalEntryID: journalID, AccountID: debitAccountID, Direction: PostingDebit, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, CreatedAt: now},
		{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, JournalEntryID: journalID, AccountID: creditAccountID, Direction: PostingCredit, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, CreatedAt: now},
	}
	for _, posting := range m.postings[journalID] {
		availableDeltaOverride := false
		if posting.AccountID == heldAccountID {
			availableDeltaOverride = true
		}
		m.applyPostingLocked(posting, journalID, now, availableDeltaOverride, 0)
	}
	return journalID
}

func (m *memoryStore) applyPostingLocked(posting Posting, journalID string, now time.Time, availableDeltaOverride bool, availableDelta int64) {
	account := m.accounts[posting.AccountID]
	delta := -posting.AmountMinor
	if (account.NormalBalance == NormalBalanceDebit && posting.Direction == PostingDebit) || (account.NormalBalance == NormalBalanceCredit && posting.Direction == PostingCredit) {
		delta = posting.AmountMinor
	}
	if !availableDeltaOverride {
		availableDelta = delta
	}
	balance := m.balances[posting.AccountID]
	balance.AvailableMinor += availableDelta
	balance.LedgerMinor += delta
	balance.LastJournalEntryID = &journalID
	balance.UpdatedAt = now
	m.balances[posting.AccountID] = balance
}

func (m *memoryStore) createHoldLocked(transfer Transfer, now time.Time) {
	transferID := transfer.ID
	m.holds[transfer.ID] = AccountHold{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: transfer.InstitutionID, AccountID: transfer.AccountID, TransferID: &transferID, AmountMinor: transfer.AmountMinor, CurrencyID: transfer.CurrencyID, Status: HoldStatusActive, Reason: "pending_outbound_transfer", Reference: transfer.ProviderReference, CreatedAt: now, UpdatedAt: now}
	balance := m.balances[transfer.AccountID]
	balance.AvailableMinor -= transfer.AmountMinor
	balance.UpdatedAt = now
	m.balances[transfer.AccountID] = balance
}

func (m *memoryStore) releaseHoldLocked(transferID string, now time.Time) {
	hold, ok := m.holds[transferID]
	if !ok || hold.Status != HoldStatusActive {
		return
	}
	hold.Status = HoldStatusReleased
	hold.UpdatedAt = now
	hold.ReleasedAt = &now
	m.holds[transferID] = hold
	balance := m.balances[hold.AccountID]
	balance.AvailableMinor += hold.AmountMinor
	balance.UpdatedAt = now
	m.balances[hold.AccountID] = balance
}

func (m *memoryStore) consumeHoldLocked(transferID string, now time.Time) {
	hold := m.holds[transferID]
	hold.Status = HoldStatusConsumed
	hold.UpdatedAt = now
	hold.ReleasedAt = &now
	m.holds[transferID] = hold
}

type spyTransferProvider struct {
	mu sync.Mutex

	nameEnquiryCalls int
	initiateCalls    int
	requeryCalls     int
	parseCalls       int

	lastNameEnquiry NameEnquiryRequest
	lastInitiate    ProviderTransferRequest
	lastRequery     string
	lastHeaders     map[string]string

	nameEnquiryResult NameEnquiryResult
	nameEnquiryErr    error
	initiateResult    ProviderTransferResult
	initiateErr       error
	initiateDelay     time.Duration
	requeryResult     ProviderTransferResult
	requeryErr        error
	webhookEvent      ProviderWebhookEvent
}

func (s *spyTransferProvider) Name() string {
	return ProviderMockNIP
}

func (s *spyTransferProvider) NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nameEnquiryCalls++
	s.lastNameEnquiry = request
	if s.nameEnquiryErr != nil {
		return nil, s.nameEnquiryErr
	}
	return copyOf(s.nameEnquiryResult), nil
}

func (s *spyTransferProvider) InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error) {
	s.mu.Lock()
	s.initiateCalls++
	s.lastInitiate = request
	err := s.initiateErr
	result := copyOf(s.initiateResult)
	delay := s.initiateDelay
	s.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *spyTransferProvider) RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requeryCalls++
	s.lastRequery = providerReference
	if s.requeryErr != nil {
		return nil, s.requeryErr
	}
	return copyOf(s.requeryResult), nil
}

func (s *spyTransferProvider) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parseCalls++
	s.lastHeaders = map[string]string{}
	for key, value := range headers {
		s.lastHeaders[key] = value
	}
	return copyOf(s.webhookEvent), nil
}

func copyOf[T any](v T) *T {
	return &v
}
