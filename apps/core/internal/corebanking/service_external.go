package corebanking

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	NameEnquiryStatusFound               = "found"
	NameEnquiryStatusNotFound            = "not_found"
	NameEnquiryStatusProviderUnavailable = "provider_unavailable"
)

type ExternalNameEnquiryInput struct {
	InstitutionID              string
	Provider                   string
	DestinationInstitutionCode string
	AccountNumber              string
	CurrencyID                 string
}

type ExternalOutboundTransferInput struct {
	InstitutionID              string
	SourceAccountID            string
	DestinationInstitutionCode string
	BankCode                   string
	DestinationAccountNumber   string
	DestinationAccountName     string
	AmountMinor                int64
	CurrencyID                 string
	IdempotencyKey             string
	Provider                   string
	Narration                  string
	Reference                  string
	Scenario                   string
}

type ExternalNameEnquiryResult struct {
	Provider                   string    `json:"provider"`
	DestinationInstitutionCode string    `json:"destination_institution_code"`
	AccountNumber              string    `json:"account_number"`
	AccountName                string    `json:"account_name"`
	ProviderReference          *string   `json:"provider_reference,omitempty"`
	Status                     string    `json:"status"`
	Message                    string    `json:"message"`
	CreatedAt                  time.Time `json:"created_at"`
}

type ExternalOutboundTransferResult struct {
	TransferID           string    `json:"transfer_id"`
	SourceAccountID      string    `json:"source_account_id"`
	Provider             string    `json:"provider"`
	ProviderReference    string    `json:"provider_reference"`
	ProviderStatus       string    `json:"provider_status"`
	LedgerStatus         string    `json:"ledger_status"`
	ReconciliationStatus string    `json:"reconciliation_status"`
	Status               string    `json:"status"`
	AmountMinor          int64     `json:"amount_minor"`
	CurrencyID           string    `json:"currency_id"`
	JournalEntryID       *string   `json:"journal_entry_id"`
	HoldID               *string   `json:"hold_id"`
	CreatedAt            time.Time `json:"created_at"`
}

func (s *Service) ExternalNameEnquiry(ctx context.Context, input ExternalNameEnquiryInput) (*ExternalNameEnquiryResult, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	providerName := strings.TrimSpace(input.Provider)
	if providerName == "" {
		providerName = ProviderMockNIP
	}
	destinationInstitutionCode := strings.TrimSpace(input.DestinationInstitutionCode)
	accountNumber := strings.TrimSpace(input.AccountNumber)
	currencyID := strings.ToUpper(strings.TrimSpace(input.CurrencyID))
	if currencyID == "" {
		currencyID = "NGN"
	}
	if destinationInstitutionCode == "" || !isTenDigitAccountNumber(accountNumber) || currencyID != "NGN" {
		return nil, ErrInvalidRequest
	}

	provider, err := s.provider(providerName)
	if err != nil {
		return nil, err
	}
	result, err := provider.NameEnquiry(ctx, NameEnquiryRequest{
		InstitutionID: institutionID,
		BankCode:      destinationInstitutionCode,
		AccountNumber: accountNumber,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidRequest) {
			return nil, ErrInvalidRequest
		}
		return externalNameEnquiryErrorResult(providerName, destinationInstitutionCode, accountNumber, err), nil
	}
	if result == nil || strings.TrimSpace(result.AccountName) == "" {
		return externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, "", nil, NameEnquiryStatusNotFound, "account_not_found"), nil
	}

	providerReference := optionalResultString(result.ProviderReference)
	return externalNameEnquiryResult(
		firstNonBlank(result.Provider, providerName),
		firstNonBlank(result.BankCode, destinationInstitutionCode),
		firstNonBlank(result.AccountNumber, accountNumber),
		strings.TrimSpace(result.AccountName),
		providerReference,
		NameEnquiryStatusFound,
		"account_found",
	), nil
}

func (s *Service) ExternalOutboundTransfer(ctx context.Context, input ExternalOutboundTransferInput) (*ExternalOutboundTransferResult, error) {
	normalized, err := normalizeExternalOutboundTransferInput(input)
	if err != nil {
		return nil, err
	}
	provider, err := s.provider(normalized.Provider)
	if err != nil {
		return nil, err
	}
	clearing, err := s.repository.GetDefaultInternalSettlementAccount(ctx, normalized.InstitutionID, normalized.CurrencyID)
	if err != nil {
		return nil, err
	}

	requestFingerprint := externalOutboundTransferRequestFingerprint(normalized)
	providerReference := firstNonBlank(normalized.Reference, normalized.IdempotencyKey)
	intentInput := RecordTransferInput{
		InstitutionID:      normalized.InstitutionID,
		AccountID:          normalized.SourceAccountID,
		ClearingAccountID:  clearing.ID,
		Direction:          TransferDirectionOutbound,
		Status:             TransferStatusPending,
		AmountMinor:        normalized.AmountMinor,
		CurrencyID:         normalized.CurrencyID,
		IdempotencyKey:     normalized.IdempotencyKey,
		Provider:           normalized.Provider,
		ProviderReference:  providerReference,
		ProviderStatus:     TransferStatusPending,
		RequestFingerprint: requestFingerprint,
		Narration:          normalized.Narration,
		RejectInsufficient: true,
		RequireAvailable:   true,
	}
	intent, created, err := s.repository.BeginExternalOutboundTransfer(ctx, intentInput)
	if err != nil {
		return nil, err
	}
	if !created {
		return s.externalOutboundTransferResult(ctx, *intent)
	}

	providerRequest := ProviderTransferRequest{
		InstitutionID:              normalized.InstitutionID,
		AccountID:                  normalized.SourceAccountID,
		DestinationInstitutionCode: normalized.DestinationInstitutionCode,
		DestinationAccountNumber:   normalized.DestinationAccountNumber,
		DestinationAccountName:     normalized.DestinationAccountName,
		AmountMinor:                normalized.AmountMinor,
		CurrencyID:                 normalized.CurrencyID,
		IdempotencyKey:             normalized.IdempotencyKey,
		ProviderReference:          providerReference,
		Narration:                  normalized.Narration,
		Scenario:                   normalized.Scenario,
	}
	providerResult, providerErr := provider.InitiateTransfer(ctx, providerRequest)
	if providerErr != nil {
		if providerTransferStatusUnknown(providerErr) {
			providerResult = providerUnknownTransferResult(provider, providerRequest)
		} else {
			providerResult = failedProviderTransferResult(provider, providerRequest)
		}
	}
	if providerResult == nil {
		providerResult = failedProviderTransferResult(provider, providerRequest)
	}
	completed, err := s.repository.CompleteExternalOutboundTransfer(ctx, intent.ID, externalOutboundCompletionInput(intentInput, *providerResult))
	if err != nil {
		return nil, err
	}
	return s.externalOutboundTransferResult(ctx, *completed)
}

func externalNameEnquiryErrorResult(providerName, destinationInstitutionCode, accountNumber string, err error) *ExternalNameEnquiryResult {
	if errors.Is(err, ErrNotFound) {
		return externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, "", nil, NameEnquiryStatusNotFound, "account_not_found")
	}
	return externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, "", nil, NameEnquiryStatusProviderUnavailable, "provider_unavailable")
}

func externalNameEnquiryResult(providerName, destinationInstitutionCode, accountNumber, accountName string, providerReference *string, status, message string) *ExternalNameEnquiryResult {
	return &ExternalNameEnquiryResult{
		Provider:                   providerName,
		DestinationInstitutionCode: destinationInstitutionCode,
		AccountNumber:              accountNumber,
		AccountName:                accountName,
		ProviderReference:          providerReference,
		Status:                     status,
		Message:                    message,
		CreatedAt:                  time.Now().UTC(),
	}
}

func normalizeExternalOutboundTransferInput(input ExternalOutboundTransferInput) (ExternalOutboundTransferInput, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return ExternalOutboundTransferInput{}, err
	}
	input.InstitutionID = institutionID
	input.SourceAccountID = strings.TrimSpace(input.SourceAccountID)
	input.DestinationInstitutionCode = firstNonBlank(input.DestinationInstitutionCode, input.BankCode)
	input.DestinationAccountNumber = strings.TrimSpace(input.DestinationAccountNumber)
	input.DestinationAccountName = strings.TrimSpace(input.DestinationAccountName)
	input.CurrencyID = strings.ToUpper(strings.TrimSpace(input.CurrencyID))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Narration = strings.TrimSpace(input.Narration)
	input.Reference = strings.TrimSpace(input.Reference)
	input.Scenario = strings.ToLower(strings.TrimSpace(input.Scenario))
	if input.CurrencyID == "" {
		input.CurrencyID = "NGN"
	}
	if input.Provider == "" {
		input.Provider = ProviderMockNIP
	}
	if input.Narration == "" {
		input.Narration = "External outbound transfer"
	}
	if _, err := uuid.Parse(input.SourceAccountID); err != nil {
		return ExternalOutboundTransferInput{}, ErrInvalidRequest
	}
	if input.AmountMinor <= 0 ||
		input.CurrencyID != "NGN" ||
		input.IdempotencyKey == "" ||
		input.DestinationInstitutionCode == "" ||
		!isTenDigitAccountNumber(input.DestinationAccountNumber) ||
		!validExternalOutboundScenario(input.Scenario) {
		return ExternalOutboundTransferInput{}, ErrInvalidRequest
	}
	return input, nil
}

func validExternalOutboundScenario(scenario string) bool {
	switch scenario {
	case "",
		MockProviderScenarioSuccess,
		MockProviderScenarioFailed,
		MockProviderScenarioPending,
		MockProviderScenarioTimeout,
		MockProviderScenarioProviderUnknown:
		return true
	default:
		return false
	}
}

func externalOutboundCompletionInput(intent RecordTransferInput, result ProviderTransferResult) RecordTransferInput {
	status := strings.ToLower(strings.TrimSpace(result.Status))
	providerStatus := strings.ToLower(strings.TrimSpace(result.ProviderStatus))
	if providerStatus == "" {
		providerStatus = status
	}
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	if status == "" {
		status = TransferStatusFailed
	}
	if providerStatus == "" {
		providerStatus = status
	}
	providerReference := firstNonBlank(result.ProviderReference, intent.ProviderReference)
	narration := firstNonBlank(result.Narration, intent.Narration)
	intent.Status = status
	intent.ProviderStatus = providerStatus
	intent.ProviderReference = providerReference
	intent.ProviderEventID = strings.TrimSpace(result.ProviderEventID)
	intent.FailureReason = strings.TrimSpace(result.FailureReason)
	intent.Narration = narration
	return intent
}

func failedProviderTransferResult(provider TransferProvider, request ProviderTransferRequest) *ProviderTransferResult {
	return &ProviderTransferResult{
		Provider:          provider.Name(),
		ProviderReference: providerTransferReference(request),
		Status:            TransferStatusFailed,
		ProviderStatus:    TransferStatusFailed,
		FailureReason:     "provider_failed",
		Narration:         strings.TrimSpace(request.Narration),
	}
}

func (s *Service) externalOutboundTransferResult(ctx context.Context, transfer Transfer) (*ExternalOutboundTransferResult, error) {
	hold, err := s.repository.GetTransferHold(ctx, transfer.InstitutionID, transfer.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var holdID *string
	if err == nil {
		holdID = &hold.ID
	}
	return &ExternalOutboundTransferResult{
		TransferID:           transfer.ID,
		SourceAccountID:      transfer.AccountID,
		Provider:             transfer.Provider,
		ProviderReference:    transfer.ProviderReference,
		ProviderStatus:       transfer.ProviderStatus,
		LedgerStatus:         transfer.LedgerStatus,
		ReconciliationStatus: transfer.ReconciliationStatus,
		Status:               transfer.Status,
		AmountMinor:          transfer.AmountMinor,
		CurrencyID:           transfer.CurrencyID,
		JournalEntryID:       transfer.JournalEntryID,
		HoldID:               holdID,
		CreatedAt:            transfer.CreatedAt,
	}, nil
}

func optionalResultString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
