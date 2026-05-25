package corebanking

import "context"

type Repository interface {
	DemoRepository
	CustomerRepository
	AccountRepository
	LedgerRepository
	TransferRepository
}

type DemoRepository interface {
	EnsureDemoData(ctx context.Context) (*SeedResult, error)
}

type CustomerRepository interface {
	CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error)
	GetCustomer(ctx context.Context, institutionID, customerID string) (*Customer, error)
}

type AccountRepository interface {
	CreateAccount(ctx context.Context, input CreateAccountInput) (*Account, error)
	ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error)
	GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error)
	GetDefaultInternalCreditSourceAccount(ctx context.Context, institutionID, currencyID string) (*Account, error)
	GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error)
	ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error)
}

type LedgerRepository interface {
	GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error)
}

type TransferRepository interface {
	GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error)
	GetTransferByIdempotency(ctx context.Context, institutionID, idempotencyKey string) (*Transfer, error)
	ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error)
	RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error)
	ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error)
}
