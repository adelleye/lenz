package corebanking

import (
	"errors"
	"time"
)

const (
	DemoInstitutionID     = "11111111-1111-1111-1111-111111111111"
	DemoBranchID          = "22222222-2222-2222-2222-222222222222"
	DemoCustomerID        = "33333333-3333-3333-3333-333333333333"
	DemoCustomerAccountID = "44444444-4444-4444-4444-444444444444"
	DemoClearingAccountID = "55555555-5555-5555-5555-555555555555"
)

const (
	AccountKindCustomer = "customer"
	AccountKindInternal = "internal"

	AccountProductStandardWallet  = "standard_wallet"
	AccountProductStandardCurrent = "standard_current"
	AccountProductStandardSavings = "standard_savings"
	AccountProductOverdraftCredit = "overdraft_credit"
	AccountProductInternal        = "internal"

	NormalBalanceDebit  = "debit"
	NormalBalanceCredit = "credit"

	PostingDebit  = "debit"
	PostingCredit = "credit"

	TransferDirectionInbound  = "inbound"
	TransferDirectionOutbound = "outbound"
	TransferDirectionReversal = "reversal"

	TransferStatusPending   = "pending"
	TransferStatusSucceeded = "succeeded"
	TransferStatusFailed    = "failed"

	TransferProviderStatusUnknown = "provider_unknown"

	LedgerStatusPending         = "pending"
	LedgerStatusPosted          = "posted"
	LedgerStatusNoPosting       = "no_posting"
	LedgerStatusReversalDeficit = "reversal_deficit"

	ReconciliationStatusPending      = "pending"
	ReconciliationStatusMatched      = "matched"
	ReconciliationStatusNoAction     = "no_action"
	ReconciliationStatusManualReview = "manual_review"

	HoldStatusActive   = "active"
	HoldStatusReleased = "released"
	HoldStatusConsumed = "consumed"

	ProviderMockNIP = "mock_nip"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrInsufficient   = errors.New("insufficient funds")
	ErrInvalidRequest = errors.New("invalid request")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("forbidden")
	ErrConflict       = errors.New("conflict")
)

const (
	DefaultTransactionHistoryLimit = 100
	MaxTransactionHistoryLimit     = 200
)

type SeedResult struct {
	Institution Institution `json:"institution"`
	Branch      Branch      `json:"branch"`
	Customer    Customer    `json:"customer"`
	Account     Account     `json:"account"`
	Clearing    Account     `json:"clearing_account"`
}

type Institution struct {
	ID         string    `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	ShortName  string    `json:"short_name" db:"short_name"`
	Code       string    `json:"code" db:"code"`
	CurrencyID string    `json:"currency_id" db:"currency_id"`
	Status     string    `json:"status" db:"status"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

type Branch struct {
	ID            string    `json:"id" db:"id"`
	InstitutionID string    `json:"institution_id" db:"institution_id"`
	Code          string    `json:"code" db:"code"`
	Name          string    `json:"name" db:"name"`
	Status        string    `json:"status" db:"status"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type Customer struct {
	ID            string    `json:"id" db:"id"`
	InstitutionID string    `json:"institution_id" db:"institution_id"`
	BranchID      string    `json:"branch_id" db:"branch_id"`
	FirstName     string    `json:"first_name" db:"first_name"`
	LastName      string    `json:"last_name" db:"last_name"`
	Email         string    `json:"email" db:"email"`
	Phone         string    `json:"phone" db:"phone"`
	Status        string    `json:"status" db:"status"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type CreateCustomerInput struct {
	InstitutionID string
	BranchID      string
	FirstName     string
	LastName      string
	Email         string
	Phone         string
}

type Account struct {
	ID            string    `json:"id" db:"id"`
	InstitutionID string    `json:"institution_id" db:"institution_id"`
	CustomerID    *string   `json:"customer_id,omitempty" db:"customer_id"`
	AccountNumber string    `json:"account_number" db:"account_number"`
	Name          string    `json:"name" db:"name"`
	Kind          string    `json:"kind" db:"kind"`
	ProductType   string    `json:"product_type" db:"product_type"`
	AllowNegative bool      `json:"allow_negative_balance" db:"allow_negative_balance"`
	CurrencyID    string    `json:"currency_id" db:"currency_id"`
	NormalBalance string    `json:"normal_balance" db:"normal_balance"`
	Status        string    `json:"status" db:"status"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type AccountBalance struct {
	AccountID          string    `json:"account_id" db:"account_id"`
	InstitutionID      string    `json:"institution_id" db:"institution_id"`
	AvailableMinor     int64     `json:"available_minor" db:"available_minor"`
	LedgerMinor        int64     `json:"ledger_minor" db:"ledger_minor"`
	CurrencyID         string    `json:"currency_id" db:"currency_id"`
	LastJournalEntryID *string   `json:"last_journal_entry_id,omitempty" db:"last_journal_entry_id"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

type Transfer struct {
	ID                   string    `json:"id" db:"id"`
	InstitutionID        string    `json:"institution_id" db:"institution_id"`
	AccountID            string    `json:"account_id" db:"account_id"`
	Direction            string    `json:"direction" db:"direction"`
	Status               string    `json:"status" db:"status"`
	ProviderStatus       string    `json:"provider_status" db:"provider_status"`
	LedgerStatus         string    `json:"ledger_status" db:"ledger_status"`
	ReconciliationStatus string    `json:"reconciliation_status" db:"reconciliation_status"`
	AmountMinor          int64     `json:"amount_minor" db:"amount_minor"`
	CurrencyID           string    `json:"currency_id" db:"currency_id"`
	IdempotencyKey       string    `json:"idempotency_key" db:"idempotency_key"`
	Provider             string    `json:"provider" db:"provider"`
	ProviderReference    string    `json:"provider_reference" db:"provider_reference"`
	ProviderEventID      *string   `json:"provider_event_id,omitempty" db:"provider_event_id"`
	JournalEntryID       *string   `json:"journal_entry_id,omitempty" db:"journal_entry_id"`
	ReversalOfTransferID *string   `json:"reversal_of_transfer_id,omitempty" db:"reversal_of_transfer_id"`
	FailureReason        *string   `json:"failure_reason,omitempty" db:"failure_reason"`
	Narration            string    `json:"narration" db:"narration"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
}

type JournalEntry struct {
	ID               string    `json:"id" db:"id"`
	InstitutionID    string    `json:"institution_id" db:"institution_id"`
	TransferID       *string   `json:"transfer_id,omitempty" db:"transfer_id"`
	EntryType        string    `json:"entry_type" db:"entry_type"`
	CurrencyID       string    `json:"currency_id" db:"currency_id"`
	Narration        string    `json:"narration" db:"narration"`
	TotalDebitMinor  int64     `json:"total_debit_minor" db:"total_debit_minor"`
	TotalCreditMinor int64     `json:"total_credit_minor" db:"total_credit_minor"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

type Posting struct {
	ID             string    `json:"id" db:"id"`
	InstitutionID  string    `json:"institution_id" db:"institution_id"`
	JournalEntryID string    `json:"journal_entry_id" db:"journal_entry_id"`
	AccountID      string    `json:"account_id" db:"account_id"`
	Direction      string    `json:"direction" db:"direction"`
	AmountMinor    int64     `json:"amount_minor" db:"amount_minor"`
	CurrencyID     string    `json:"currency_id" db:"currency_id"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type AccountHold struct {
	ID            string     `json:"id" db:"id"`
	InstitutionID string     `json:"institution_id" db:"institution_id"`
	AccountID     string     `json:"account_id" db:"account_id"`
	TransferID    string     `json:"transfer_id" db:"transfer_id"`
	AmountMinor   int64      `json:"amount_minor" db:"amount_minor"`
	CurrencyID    string     `json:"currency_id" db:"currency_id"`
	Status        string     `json:"status" db:"status"`
	Reason        string     `json:"reason" db:"reason"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
	ReleasedAt    *time.Time `json:"released_at,omitempty" db:"released_at"`
}

type JournalWithPostings struct {
	JournalEntry JournalEntry `json:"journal_entry"`
	Postings     []Posting    `json:"postings"`
	Balanced     bool         `json:"balanced"`
}

type Transaction struct {
	ID             string    `json:"id" db:"id"`
	TransferID     string    `json:"transfer_id" db:"transfer_id"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty" db:"journal_entry_id"`
	AccountID      string    `json:"account_id" db:"account_id"`
	Direction      string    `json:"direction" db:"direction"`
	Status         string    `json:"status" db:"status"`
	AmountMinor    int64     `json:"amount_minor" db:"amount_minor"`
	SignedMinor    int64     `json:"signed_minor" db:"signed_minor"`
	CurrencyID     string    `json:"currency_id" db:"currency_id"`
	Narration      string    `json:"narration" db:"narration"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type ListTransactionsOptions struct {
	Limit           int
	BeforeCreatedAt *time.Time
}

type TransferRequest struct {
	InstitutionID     string `json:"institution_id"`
	AccountID         string `json:"account_id"`
	AmountMinor       int64  `json:"amount_minor"`
	CurrencyID        string `json:"currency_id"`
	IdempotencyKey    string `json:"idempotency_key"`
	ProviderEventID   string `json:"provider_event_id"`
	ProviderReference string `json:"provider_reference"`
	Status            string `json:"status"`
	Narration         string `json:"narration"`
	Scenario          string `json:"scenario"`
	DelaySeconds      int64  `json:"delay_seconds"`
}

type RecordTransferInput struct {
	InstitutionID        string
	AccountID            string
	ClearingAccountID    string
	Direction            string
	Status               string
	AmountMinor          int64
	CurrencyID           string
	IdempotencyKey       string
	Provider             string
	ProviderReference    string
	ProviderEventID      string
	ProviderStatus       string
	ReversalOfTransferID string
	FailureReason        string
	Narration            string
}

type ReverseTransferInput struct {
	InstitutionID     string
	TransferID        string
	IdempotencyKey    string
	Provider          string
	ProviderReference string
	ProviderEventID   string
	FailureReason     string
	Narration         string
}
