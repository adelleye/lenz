package corebanking

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
)

type Service struct {
	repository Repository
	providers  map[string]TransferProvider
}

func NewService(repository Repository, providers ...TransferProvider) *Service {
	s := &Service{repository: repository, providers: map[string]TransferProvider{}}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		s.providers[provider.Name()] = provider
	}
	return s
}

func (s *Service) SeedDemo(ctx context.Context) (*SeedResult, error) {
	return s.repository.EnsureDemoData(ctx)
}

func (s *Service) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	input.InstitutionID = institutionID
	input.BranchID = strings.TrimSpace(input.BranchID)
	input.CustomerType = strings.ToLower(strings.TrimSpace(input.CustomerType))
	input.FirstName = strings.TrimSpace(input.FirstName)
	input.LastName = strings.TrimSpace(input.LastName)
	input.BusinessName = strings.TrimSpace(input.BusinessName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Phone = strings.TrimSpace(input.Phone)
	if input.BranchID == "" {
		return nil, ErrInvalidRequest
	}
	switch input.CustomerType {
	case CustomerTypeIndividual:
		if input.FirstName == "" || input.LastName == "" {
			return nil, ErrInvalidRequest
		}
		input.BusinessName = ""
	case CustomerTypeBusiness:
		if input.BusinessName == "" {
			return nil, ErrInvalidRequest
		}
	default:
		return nil, ErrInvalidRequest
	}
	if input.Email != "" {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email {
			return nil, ErrInvalidRequest
		}
	}
	input.KYCTier = CustomerKYCTier1
	input.BVNStatus = CustomerIdentityStatusNotCollected
	input.NINStatus = CustomerIdentityStatusNotCollected
	return s.repository.CreateCustomer(ctx, input)
}

func (s *Service) GetCustomer(ctx context.Context, institutionID, customerID string) (*Customer, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, ErrInvalidRequest
	}
	return s.repository.GetCustomer(ctx, institutionID, customerID)
}

func (s *Service) CreateAccount(ctx context.Context, input CreateAccountInput) (*Account, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	input.InstitutionID = institutionID
	input.CustomerID = strings.TrimSpace(input.CustomerID)
	input.AccountNumber = strings.TrimSpace(input.AccountNumber)
	input.Name = strings.TrimSpace(input.Name)
	input.ProductType = strings.ToLower(strings.TrimSpace(input.ProductType))
	input.CurrencyID = strings.ToUpper(strings.TrimSpace(input.CurrencyID))
	if input.ProductType == "" {
		input.ProductType = AccountProductStandardWallet
	}
	if input.CurrencyID == "" {
		input.CurrencyID = "NGN"
	}
	if _, err := uuid.Parse(input.CustomerID); err != nil {
		return nil, ErrInvalidRequest
	}
	if input.Name == "" || !isTenDigitAccountNumber(input.AccountNumber) {
		return nil, ErrInvalidRequest
	}
	if !validCustomerAccountProduct(input.ProductType) || input.CurrencyID != "NGN" || input.AllowNegativeBalance {
		return nil, ErrInvalidRequest
	}
	return s.repository.CreateAccount(ctx, input)
}

func (s *Service) GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, ErrInvalidRequest
	}
	return s.repository.GetAccount(ctx, institutionID, accountID)
}

func (s *Service) ListCustomerAccounts(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, ErrInvalidRequest
	}
	accounts, err := s.repository.ListAccountsByCustomer(ctx, institutionID, customerID)
	if accounts == nil {
		accounts = []Account{}
	}
	return accounts, err
}

func (s *Service) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	accountID = strings.TrimSpace(accountID)
	if _, err := uuid.Parse(accountID); err != nil {
		return nil, ErrInvalidRequest
	}
	return s.repository.GetBalance(ctx, institutionID, accountID)
}

func (s *Service) InternalCredit(ctx context.Context, input InternalCreditInput) (*Transfer, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	input.InstitutionID = institutionID
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.SourceAccountID = strings.TrimSpace(input.SourceAccountID)
	input.CurrencyID = strings.ToUpper(strings.TrimSpace(input.CurrencyID))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Reference = strings.TrimSpace(input.Reference)
	input.Narration = strings.TrimSpace(input.Narration)
	if input.Narration == "" {
		input.Narration = "Internal credit"
	}
	if input.AmountMinor <= 0 || input.IdempotencyKey == "" || input.CurrencyID != "NGN" {
		return nil, ErrInvalidRequest
	}
	if _, err := uuid.Parse(input.AccountID); err != nil {
		return nil, ErrInvalidRequest
	}
	if input.SourceAccountID != "" {
		if _, err := uuid.Parse(input.SourceAccountID); err != nil {
			return nil, ErrInvalidRequest
		}
	}

	account, err := s.repository.GetAccount(ctx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if account.Kind != AccountKindCustomer || account.Status != "active" || account.CurrencyID != input.CurrencyID {
		return nil, ErrInvalidRequest
	}

	source := (*Account)(nil)
	if input.SourceAccountID == "" {
		source, err = s.repository.GetDefaultInternalCreditSourceAccount(ctx, input.InstitutionID, input.CurrencyID)
	} else {
		source, err = s.repository.GetAccount(ctx, input.InstitutionID, input.SourceAccountID)
	}
	if err != nil {
		return nil, err
	}
	if !validInternalCreditSourceAccount(*source, input.InstitutionID, input.CurrencyID) {
		return nil, ErrInvalidRequest
	}

	return s.repository.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     input.InstitutionID,
		AccountID:         account.ID,
		ClearingAccountID: source.ID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       input.AmountMinor,
		CurrencyID:        input.CurrencyID,
		IdempotencyKey:    input.IdempotencyKey,
		Provider:          ProviderLedgerInternal,
		ProviderReference: input.Reference,
		ProviderStatus:    TransferStatusSucceeded,
		Narration:         input.Narration,
	})
}

func (s *Service) GetTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.repository.ListTransactions(ctx, institutionID, accountID, normalizeListTransactionsOptions(options))
}

func (s *Service) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.repository.GetTransfer(ctx, institutionID, transferID)
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
	return s.repository.ListTransfers(ctx, institutionID)
}

func (s *Service) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	institutionID, err := requireInstitutionID(institutionID)
	if err != nil {
		return nil, err
	}
	return s.repository.GetJournal(ctx, institutionID, journalEntryID)
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
	existing, err := s.repository.GetTransferByIdempotency(ctx, req.InstitutionID, req.IdempotencyKey)
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
	return s.repository.ReverseTransfer(ctx, ReverseTransferInput{
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
		return s.repository.ReverseTransfer(ctx, ReverseTransferInput{
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
	return s.repository.RecordTransfer(ctx, RecordTransferInput{
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
	return s.repository.RecordTransfer(ctx, RecordTransferInput{
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

func validCustomerAccountProduct(productType string) bool {
	switch productType {
	case AccountProductStandardWallet, AccountProductStandardSavings, AccountProductStandardCurrent:
		return true
	default:
		return false
	}
}

func validInternalCreditSourceAccount(account Account, institutionID, currencyID string) bool {
	return account.InstitutionID == institutionID &&
		account.Kind == AccountKindInternal &&
		account.ProductType == AccountProductInternal &&
		account.AllowNegative &&
		account.CurrencyID == currencyID &&
		account.NormalBalance == NormalBalanceDebit &&
		account.Status == "active"
}

func isTenDigitAccountNumber(accountNumber string) bool {
	if len(accountNumber) != 10 {
		return false
	}
	for _, char := range accountNumber {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
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
