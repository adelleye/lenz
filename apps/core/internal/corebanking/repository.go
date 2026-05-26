package corebanking

import "context"

type Repository interface {
	EnsureDemoData(ctx context.Context) (*SeedResult, error)

	CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error)
	GetCustomer(ctx context.Context, institutionID, customerID string) (*Customer, error)

	CreateAccount(ctx context.Context, input CreateAccountInput) (*Account, error)
	ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error)
	GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error)
	GetAccountByNumber(ctx context.Context, institutionID, accountNumber string) (*Account, error)
	SetAccountStatus(ctx context.Context, input AccountControlInput, status string, allowedCurrentStatuses ...string) (*Account, error)
	GetDefaultInternalSettlementAccount(ctx context.Context, institutionID, currencyID string) (*Account, error)
	GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error)
	ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error)
	PlaceAccountLien(ctx context.Context, input AccountLienInput) (*AccountHold, error)
	ReleaseAccountLien(ctx context.Context, input ReleaseLienInput) (*AccountHold, error)

	GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error)

	GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error)
	GetTransferByIdempotency(ctx context.Context, institutionID, idempotencyKey string) (*Transfer, error)
	ListTransfers(ctx context.Context, institutionID string, options ListTransfersOptions) ([]Transfer, error)
	RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error)
	RecordProviderEventReview(ctx context.Context, input RecordProviderEventReviewInput) (*Transfer, error)
	BeginExternalOutboundTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, bool, error)
	CompleteExternalOutboundTransfer(ctx context.Context, transferID string, input RecordTransferInput) (*Transfer, error)
	CompleteExternalTransferRequery(ctx context.Context, transferID string, input RecordTransferInput) (*Transfer, error)
	GetTransferHold(ctx context.Context, institutionID, transferID string) (*AccountHold, error)
	ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error)

	ListReconciliationItems(ctx context.Context, institutionID string, options ListReconciliationItemsOptions) ([]ReconciliationItem, error)
	GetReconciliationItem(ctx context.Context, institutionID, transferID string) (*ReconciliationItem, error)
	MarkReconciliationItemReviewed(ctx context.Context, input MarkReconciliationItemReviewedInput) (*ReconciliationItem, error)

	ListAuditEvents(ctx context.Context, institutionID string, options ListAuditEventsOptions) ([]AuditEvent, error)
}
