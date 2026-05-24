package corebanking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Service struct {
	store     Repository
	providers map[string]TransferProvider
}

func NewService(store Repository, providers ...TransferProvider) *Service {
	s := &Service{store: store, providers: map[string]TransferProvider{}}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		s.providers[provider.Name()] = provider
	}
	return s
}

func (s *Service) SeedDemo(ctx context.Context) (*SeedResult, error) {
	return s.store.EnsureDemoData(ctx)
}

func (s *Service) ListCustomerAccounts(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.ListAccountsByCustomer(ctx, institutionID, customerID)
}

func (s *Service) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.GetBalance(ctx, institutionID, accountID)
}

func (s *Service) GetTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.ListTransactions(ctx, institutionID, accountID, normalizeListTransactionsOptions(options))
}

func (s *Service) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.GetTransfer(ctx, institutionID, transferID)
}

func (s *Service) RequeryTransfer(ctx context.Context, institutionID, transferID string) (*ProviderTransferResult, error) {
	transfer, err := s.GetTransfer(ctx, institutionID, transferID)
	if err != nil {
		return nil, err
	}
	provider, err := s.provider(transfer.Provider)
	if err != nil {
		return nil, err
	}
	return provider.RequeryTransfer(ctx, transfer.ProviderReference)
}

func (s *Service) ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.ListTransfers(ctx, institutionID)
}

func (s *Service) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.GetJournal(ctx, institutionID, journalEntryID)
}

func (s *Service) MockInbound(ctx context.Context, req TransferRequest) (*Transfer, error) {
	headers := map[string]string{}
	if req.InstitutionID != "" {
		headers["X-Institution-ID"] = req.InstitutionID
	}
	if req.IdempotencyKey != "" {
		headers["Idempotency-Key"] = req.IdempotencyKey
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return s.MockProviderWebhook(ctx, ProviderMockNIP, payload, headers)
}

func (s *Service) MockOutbound(ctx context.Context, req TransferRequest) (*Transfer, error) {
	if err := normalizeTransferRequest(&req); err != nil {
		return nil, err
	}
	existing, err := s.store.GetTransferByIdempotency(ctx, req.InstitutionID, req.IdempotencyKey)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	provider, err := s.provider(ProviderMockNIP)
	if err != nil {
		return nil, err
	}
	providerRequest := ProviderTransferRequest{
		InstitutionID:     req.InstitutionID,
		AccountID:         req.AccountID,
		AmountMinor:       req.AmountMinor,
		CurrencyID:        req.CurrencyID,
		IdempotencyKey:    req.IdempotencyKey,
		ProviderReference: req.ProviderReference,
		ProviderEventID:   req.ProviderEventID,
		Status:            req.Status,
		Narration:         req.Narration,
		Scenario:          req.Scenario,
		DelaySeconds:      req.DelaySeconds,
	}
	result, err := provider.InitiateTransfer(ctx, providerRequest)
	if err != nil {
		if !providerTransferStatusUnknown(err) {
			return nil, err
		}
		result = providerUnknownTransferResult(provider, providerRequest)
	}
	return s.recordProviderTransfer(ctx, TransferDirectionOutbound, req, *result)
}

func (s *Service) MockProviderWebhook(ctx context.Context, providerName string, payload []byte, headers map[string]string) (*Transfer, error) {
	provider, err := s.provider(providerName)
	if err != nil {
		return nil, err
	}
	event, err := provider.ParseWebhook(ctx, payload, headers)
	if err != nil {
		return nil, err
	}
	return s.recordProviderWebhookEvent(ctx, *event)
}

func (s *Service) ReverseTransfer(ctx context.Context, institutionID, transferID, idempotencyKey string) (*Transfer, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, ErrInvalidRequest
	}
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.store.ReverseTransfer(ctx, ReverseTransferInput{
		InstitutionID:  institutionID,
		TransferID:     strings.TrimSpace(transferID),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
}

func (s *Service) provider(name string) (TransferProvider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidRequest
	}
	provider, ok := s.providers[name]
	if !ok {
		return nil, ErrInvalidRequest
	}
	return provider, nil
}

func (s *Service) recordProviderWebhookEvent(ctx context.Context, event ProviderWebhookEvent) (*Transfer, error) {
	event.Provider = strings.TrimSpace(event.Provider)
	if event.Provider == "" {
		return nil, ErrInvalidRequest
	}
	institutionID, err := requireInstitutionID(event.InstitutionID)
	if err != nil {
		return nil, err
	}
	event.InstitutionID = institutionID
	event.Direction = strings.ToLower(strings.TrimSpace(event.Direction))
	event.Status = strings.ToLower(strings.TrimSpace(event.Status))
	event.CurrencyID = strings.ToUpper(strings.TrimSpace(event.CurrencyID))
	event.IdempotencyKey = strings.TrimSpace(event.IdempotencyKey)
	event.ProviderEventID = strings.TrimSpace(event.ProviderEventID)
	event.ProviderReference = strings.TrimSpace(event.ProviderReference)
	event.FailureReason = strings.TrimSpace(event.FailureReason)
	event.Narration = strings.TrimSpace(event.Narration)
	if event.CurrencyID == "" {
		event.CurrencyID = "NGN"
	}
	if event.Status == "" {
		event.Status = TransferStatusSucceeded
	}
	if event.Narration == "" {
		event.Narration = "Provider webhook"
	}
	if event.IdempotencyKey == "" || event.ProviderEventID == "" {
		return nil, ErrInvalidRequest
	}
	if !validTransferStatus(event.Status) {
		return nil, ErrInvalidRequest
	}
	if event.Direction == TransferDirectionReversal {
		if strings.TrimSpace(event.ReversalOfTransferID) == "" {
			return nil, ErrInvalidRequest
		}
		return s.store.ReverseTransfer(ctx, ReverseTransferInput{
			InstitutionID:     event.InstitutionID,
			TransferID:        strings.TrimSpace(event.ReversalOfTransferID),
			IdempotencyKey:    event.IdempotencyKey,
			Provider:          event.Provider,
			ProviderReference: event.ProviderReference,
			ProviderEventID:   event.ProviderEventID,
			FailureReason:     event.FailureReason,
			Narration:         event.Narration,
		})
	}
	if event.Direction != TransferDirectionInbound && event.Direction != TransferDirectionOutbound {
		return nil, ErrInvalidRequest
	}
	if event.AccountID == "" || event.AmountMinor <= 0 {
		return nil, ErrInvalidRequest
	}
	return s.store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     event.InstitutionID,
		AccountID:         event.AccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         event.Direction,
		Status:            event.Status,
		AmountMinor:       event.AmountMinor,
		CurrencyID:        event.CurrencyID,
		IdempotencyKey:    event.IdempotencyKey,
		Provider:          event.Provider,
		ProviderReference: event.ProviderReference,
		ProviderEventID:   event.ProviderEventID,
		FailureReason:     event.FailureReason,
		Narration:         event.Narration,
	})
}

func (s *Service) recordProviderTransfer(ctx context.Context, direction string, req TransferRequest, result ProviderTransferResult) (*Transfer, error) {
	providerName := strings.TrimSpace(result.Provider)
	if providerName == "" {
		providerName = ProviderMockNIP
	}
	status := strings.ToLower(strings.TrimSpace(result.Status))
	providerStatus := strings.ToLower(strings.TrimSpace(result.ProviderStatus))
	if status == TransferProviderStatusUnknown {
		status = TransferStatusPending
		providerStatus = TransferProviderStatusUnknown
	}
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	if status == "" {
		status = req.Status
	}
	if providerStatus == "" {
		providerStatus = status
	}
	if !validTransferStatus(status) {
		return nil, ErrInvalidRequest
	}
	if !validProviderStatus(providerStatus) {
		return nil, ErrInvalidRequest
	}
	failureReason := strings.TrimSpace(result.FailureReason)
	narration := strings.TrimSpace(result.Narration)
	if narration == "" {
		narration = req.Narration
	}
	providerReference := strings.TrimSpace(result.ProviderReference)
	if providerReference == "" {
		providerReference = req.ProviderReference
	}
	providerEventID := strings.TrimSpace(result.ProviderEventID)
	if providerEventID == "" {
		providerEventID = req.ProviderEventID
	}
	return s.store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     req.InstitutionID,
		AccountID:         req.AccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         direction,
		Status:            status,
		AmountMinor:       req.AmountMinor,
		CurrencyID:        req.CurrencyID,
		IdempotencyKey:    req.IdempotencyKey,
		Provider:          providerName,
		ProviderReference: providerReference,
		ProviderEventID:   providerEventID,
		ProviderStatus:    providerStatus,
		FailureReason:     failureReason,
		Narration:         narration,
	})
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

func validTransferStatus(status string) bool {
	switch status {
	case TransferStatusSucceeded, TransferStatusPending, TransferStatusFailed:
		return true
	default:
		return false
	}
}

func validProviderStatus(status string) bool {
	switch status {
	case TransferStatusSucceeded, TransferStatusPending, TransferStatusFailed, TransferProviderStatusUnknown:
		return true
	default:
		return false
	}
}

func requireInstitutionID(institutionID string) (string, error) {
	institutionID = strings.TrimSpace(institutionID)
	if institutionID == "" {
		return "", ErrInvalidRequest
	}
	return institutionID, nil
}

func institutionIDOrDemo(institutionID string) string {
	institutionID = strings.TrimSpace(institutionID)
	if institutionID == "" {
		return DemoInstitutionID
	}
	return institutionID
}

func normalizeListTransactionsOptions(options ListTransactionsOptions) ListTransactionsOptions {
	if options.Limit <= 0 {
		options.Limit = DefaultTransactionHistoryLimit
	}
	if options.Limit > MaxTransactionHistoryLimit {
		options.Limit = MaxTransactionHistoryLimit
	}
	return options
}
