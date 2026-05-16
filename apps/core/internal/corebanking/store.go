package corebanking

import "context"

type Store interface {
	EnsureDemoData(ctx context.Context) (*SeedResult, error)
	ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error)
	GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error)
	GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error)
	GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error)
	ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error)
	GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error)
	ListTransactions(ctx context.Context, institutionID, accountID string) ([]Transaction, error)
	RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error)
	ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error)
}
