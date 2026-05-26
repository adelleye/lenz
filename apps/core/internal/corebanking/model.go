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

	AccountStatusActive      = "active"
	AccountStatusFrozen      = "frozen"
	AccountStatusPostNoDebit = "post_no_debit"
	AccountStatusClosed      = "closed"

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

	TransactionDirectionCredit = "credit"
	TransactionDirectionDebit  = "debit"

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

	CustomerTypeIndividual = "individual"
	CustomerTypeBusiness   = "business"

	CustomerKYCTier1 = "tier1"

	CustomerIdentityStatusNotCollected = "not_collected"

	ProviderMockNIP        = "mock_nip"
	ProviderLedgerInternal = "ledger_internal"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrInsufficient   = errors.New("insufficient funds")
	ErrInvalidRequest = errors.New("invalid request")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("forbidden")
	ErrConflict       = errors.New("conflict")
	ErrDataIntegrity  = errors.New("data integrity error")

	ErrUnsupportedProvider = errors.Join(ErrInvalidRequest, errors.New("unsupported provider"))
)

const (
	DefaultTransactionHistoryLimit = 100
	MaxTransactionHistoryLimit     = 200
)

const (
	DefaultReconciliationItemLimit           = DefaultTransactionHistoryLimit
	MaxReconciliationItemLimit               = MaxTransactionHistoryLimit
	DefaultReconciliationStalePendingMinutes = 24 * 60

	ReconciliationReviewStatusReviewed               = "reviewed"
	ReconciliationReviewStatusResolvedNoAction       = "resolved_no_action"
	ReconciliationReviewStatusManualFollowupRequired = "manual_followup_required"

	ReconciliationActionRequeryProvider                = "requery_provider"
	ReconciliationActionInspectJournal                 = "inspect_journal"
	ReconciliationActionContactProvider                = "contact_provider"
	ReconciliationActionManualCustomerReceivableReview = "manual_customer_receivable_review"
	ReconciliationActionNoAction                       = "no_action"
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
	CustomerType  string    `json:"customer_type" db:"customer_type"`
	FirstName     string    `json:"first_name" db:"first_name"`
	LastName      string    `json:"last_name" db:"last_name"`
	BusinessName  *string   `json:"business_name,omitempty" db:"business_name"`
	Email         string    `json:"email" db:"email"`
	Phone         string    `json:"phone" db:"phone"`
	Status        string    `json:"status" db:"status"`
	KYCTier       string    `json:"kyc_tier" db:"kyc_tier"`
	BVNStatus     string    `json:"bvn_status" db:"bvn_status"`
	NINStatus     string    `json:"nin_status" db:"nin_status"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type CreateCustomerInput struct {
	InstitutionID string
	BranchID      string
	CustomerType  string
	FirstName     string
	LastName      string
	BusinessName  string
	Email         string
	Phone         string
	KYCTier       string
	BVNStatus     string
	NINStatus     string
}

type CreateAccountInput struct {
	InstitutionID        string
	CustomerID           string
	AccountNumber        string
	Name                 string
	ProductType          string
	CurrencyID           string
	AllowNegativeBalance bool
}

type InternalCreditInput struct {
	InstitutionID   string
	AccountID       string
	SourceAccountID string
	AmountMinor     int64
	CurrencyID      string
	IdempotencyKey  string
	Narration       string
	Reference       string
}

type InternalDebitInput struct {
	InstitutionID        string
	AccountID            string
	DestinationAccountID string
	AmountMinor          int64
	CurrencyID           string
	IdempotencyKey       string
	Narration            string
	Reference            string
}

type InternalTransferInput struct {
	InstitutionID        string
	SourceAccountID      string
	DestinationAccountID string
	AmountMinor          int64
	CurrencyID           string
	IdempotencyKey       string
	Narration            string
	Reference            string
}

type AccountControlInput struct {
	InstitutionID string
	AccountID     string
	Reference     string
	Reason        string
}

type AccountLienInput struct {
	InstitutionID string
	AccountID     string
	AmountMinor   int64
	CurrencyID    string
	Reference     string
	Reason        string
}

type ReleaseLienInput struct {
	InstitutionID string
	AccountID     string
	LienID        string
	Reference     string
	Reason        string
}

type AuditEvent struct {
	ID             string            `json:"id" db:"id"`
	InstitutionID  string            `json:"institution_id" db:"institution_id"`
	ActorType      string            `json:"actor_type" db:"actor_type"`
	ActorID        string            `json:"actor_id" db:"actor_id"`
	RequestID      string            `json:"request_id" db:"request_id"`
	Action         string            `json:"action" db:"action"`
	EntityType     string            `json:"entity_type" db:"entity_type"`
	EntityID       string            `json:"entity_id" db:"entity_id"`
	CustomerID     *string           `json:"customer_id,omitempty" db:"customer_id"`
	AccountID      *string           `json:"account_id,omitempty" db:"account_id"`
	TransferID     *string           `json:"transfer_id,omitempty" db:"transfer_id"`
	JournalEntryID *string           `json:"journal_entry_id,omitempty" db:"journal_entry_id"`
	IdempotencyKey *string           `json:"idempotency_key,omitempty" db:"idempotency_key"`
	Reference      *string           `json:"reference,omitempty" db:"reference"`
	OldStatus      *string           `json:"old_status,omitempty" db:"old_status"`
	NewStatus      *string           `json:"new_status,omitempty" db:"new_status"`
	Metadata       map[string]string `json:"metadata" db:"-"`
	CreatedAt      time.Time         `json:"created_at" db:"created_at"`
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
	RequestFingerprint   string    `json:"-" db:"request_fingerprint"`
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
	TransferID    *string    `json:"transfer_id,omitempty" db:"transfer_id"`
	AmountMinor   int64      `json:"amount_minor" db:"amount_minor"`
	CurrencyID    string     `json:"currency_id" db:"currency_id"`
	Status        string     `json:"status" db:"status"`
	Reason        string     `json:"reason" db:"reason"`
	Reference     string     `json:"reference" db:"reference"`
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
	ID                    string    `json:"id" db:"id"`
	TransferID            string    `json:"transfer_id" db:"transfer_id"`
	JournalEntryID        *string   `json:"journal_entry_id,omitempty" db:"journal_entry_id"`
	AccountID             string    `json:"account_id" db:"account_id"`
	InstitutionID         string    `json:"institution_id" db:"institution_id"`
	Direction             string    `json:"direction" db:"direction"`
	Status                string    `json:"status" db:"status"`
	LedgerStatus          string    `json:"ledger_status" db:"ledger_status"`
	ProviderStatus        string    `json:"provider_status" db:"provider_status"`
	ReconciliationStatus  string    `json:"reconciliation_status" db:"reconciliation_status"`
	AmountMinor           int64     `json:"amount_minor" db:"amount_minor"`
	SignedAmountMinor     int64     `json:"signed_amount_minor" db:"signed_amount_minor"`
	CurrencyID            string    `json:"currency_id" db:"currency_id"`
	Narration             string    `json:"narration" db:"narration"`
	CounterpartyAccountID *string   `json:"counterparty_account_id,omitempty" db:"counterparty_account_id"`
	Provider              string    `json:"provider" db:"provider"`
	ProviderReference     string    `json:"provider_reference" db:"provider_reference"`
	CreatedAt             time.Time `json:"created_at" db:"created_at"`
}

type ListTransactionsOptions struct {
	Limit            int
	BeforeCreatedAt  *time.Time
	BeforeTransferID string
}

type ReconciliationItem struct {
	TransferID            string     `json:"transfer_id" db:"transfer_id"`
	InstitutionID         string     `json:"institution_id" db:"institution_id"`
	AccountID             string     `json:"account_id" db:"account_id"`
	Direction             string     `json:"direction" db:"direction"`
	Status                string     `json:"status" db:"status"`
	Provider              string     `json:"provider" db:"provider"`
	ProviderReference     string     `json:"provider_reference" db:"provider_reference"`
	ProviderEventID       *string    `json:"provider_event_id,omitempty" db:"provider_event_id"`
	ProviderStatus        string     `json:"provider_status" db:"provider_status"`
	LedgerStatus          string     `json:"ledger_status" db:"ledger_status"`
	ReconciliationStatus  string     `json:"reconciliation_status" db:"reconciliation_status"`
	AmountMinor           int64      `json:"amount_minor" db:"amount_minor"`
	CurrencyID            string     `json:"currency_id" db:"currency_id"`
	FailureReason         *string    `json:"failure_reason,omitempty" db:"failure_reason"`
	JournalEntryID        *string    `json:"journal_entry_id,omitempty" db:"journal_entry_id"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at" db:"updated_at"`
	ReviewReason          string     `json:"review_reason" db:"-"`
	RecommendedNextAction string     `json:"recommended_next_action" db:"-"`
	ReviewStatus          *string    `json:"review_status,omitempty" db:"review_status"`
	ReviewNote            *string    `json:"review_note,omitempty" db:"review_note"`
	ReviewedAt            *time.Time `json:"reviewed_at,omitempty" db:"reviewed_at"`
	ReviewedBy            *string    `json:"reviewed_by,omitempty" db:"reviewed_by"`
}

type ListReconciliationItemsOptions struct {
	Limit                int
	BeforeCreatedAt      *time.Time
	BeforeTransferID     string
	Status               string
	ProviderStatus       string
	LedgerStatus         string
	ReconciliationStatus string
	StalePendingMinutes  int
}

type MarkReconciliationItemReviewedInput struct {
	InstitutionID    string
	TransferID       string
	ResolutionNote   string
	ResolutionStatus string
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
	RequestFingerprint   string
	FailureReason        string
	Narration            string
	RejectInsufficient   bool
	RequireAvailable     bool
}

type RecordProviderEventReviewInput struct {
	InstitutionID        string
	AccountID            string
	Direction            string
	Status               string
	ProviderStatus       string
	AmountMinor          int64
	CurrencyID           string
	IdempotencyKey       string
	Provider             string
	ProviderReference    string
	ProviderEventID      string
	RequestFingerprint   string
	FailureReason        string
	Narration            string
	ReserveProviderEvent bool
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
