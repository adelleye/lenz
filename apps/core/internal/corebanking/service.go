package corebanking

import (
	"context"
	"strings"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) SeedDemo(ctx context.Context) (*SeedResult, error) {
	return s.store.EnsureDemoData(ctx)
}

func (s *Service) ListCustomerAccounts(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	return s.store.ListAccountsByCustomer(ctx, institutionIDOrDemo(institutionID), customerID)
}

func (s *Service) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	return s.store.GetBalance(ctx, institutionIDOrDemo(institutionID), accountID)
}

func (s *Service) GetTransactions(ctx context.Context, institutionID, accountID string) ([]Transaction, error) {
	return s.store.ListTransactions(ctx, institutionIDOrDemo(institutionID), accountID)
}

func (s *Service) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	return s.store.GetTransfer(ctx, institutionIDOrDemo(institutionID), transferID)
}

func (s *Service) ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error) {
	return s.store.ListTransfers(ctx, institutionIDOrDemo(institutionID))
}

func (s *Service) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	return s.store.GetJournal(ctx, institutionIDOrDemo(institutionID), journalEntryID)
}

func (s *Service) MockInbound(ctx context.Context, req TransferRequest) (*Transfer, error) {
	if err := normalizeTransferRequest(&req); err != nil {
		return nil, err
	}
	if req.ProviderEventID == "" {
		return nil, ErrInvalidRequest
	}
	return s.store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     institutionIDOrDemo(req.InstitutionID),
		AccountID:         req.AccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            req.Status,
		AmountMinor:       req.AmountMinor,
		CurrencyID:        req.CurrencyID,
		IdempotencyKey:    req.IdempotencyKey,
		Provider:          ProviderMockNIP,
		ProviderReference: req.ProviderReference,
		ProviderEventID:   req.ProviderEventID,
		Narration:         req.Narration,
	})
}

func (s *Service) MockOutbound(ctx context.Context, req TransferRequest) (*Transfer, error) {
	if err := normalizeTransferRequest(&req); err != nil {
		return nil, err
	}
	return s.store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     institutionIDOrDemo(req.InstitutionID),
		AccountID:         req.AccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            req.Status,
		AmountMinor:       req.AmountMinor,
		CurrencyID:        req.CurrencyID,
		IdempotencyKey:    req.IdempotencyKey,
		Provider:          ProviderMockNIP,
		ProviderReference: req.ProviderReference,
		ProviderEventID:   req.ProviderEventID,
		Narration:         req.Narration,
	})
}

func (s *Service) ReverseTransfer(ctx context.Context, institutionID, transferID, idempotencyKey string) (*Transfer, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, ErrInvalidRequest
	}
	return s.store.ReverseTransfer(ctx, institutionIDOrDemo(institutionID), transferID, idempotencyKey)
}

func normalizeTransferRequest(req *TransferRequest) error {
	req.InstitutionID = institutionIDOrDemo(req.InstitutionID)
	req.AccountID = strings.TrimSpace(req.AccountID)
	req.CurrencyID = strings.ToUpper(strings.TrimSpace(req.CurrencyID))
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.ProviderEventID = strings.TrimSpace(req.ProviderEventID)
	req.ProviderReference = strings.TrimSpace(req.ProviderReference)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.Narration = strings.TrimSpace(req.Narration)
	if req.CurrencyID == "" {
		req.CurrencyID = "NGN"
	}
	if req.Status == "" {
		req.Status = TransferStatusSucceeded
	}
	if req.Narration == "" {
		req.Narration = "Mock transfer"
	}
	if req.AccountID == "" || req.AmountMinor <= 0 || req.IdempotencyKey == "" {
		return ErrInvalidRequest
	}
	switch req.Status {
	case TransferStatusSucceeded, TransferStatusPending, TransferStatusFailed:
		return nil
	default:
		return ErrInvalidRequest
	}
}

func institutionIDOrDemo(institutionID string) string {
	institutionID = strings.TrimSpace(institutionID)
	if institutionID == "" {
		return DemoInstitutionID
	}
	return institutionID
}
