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

type ExternalInboundEventInput struct {
	InstitutionID string
	Provider      string
	Payload       []byte
	Headers       map[string]string
}

type ExternalTransferRequeryInput struct {
	InstitutionID     string
	TransferID        string
	ProviderReference string
	Scenario          string
	Note              string
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

type ExternalInboundEventResult struct {
	TransferID           *string `json:"transfer_id"`
	ProviderEventID      string  `json:"provider_event_id"`
	ProviderReference    string  `json:"provider_reference"`
	ProviderStatus       string  `json:"provider_status"`
	LedgerStatus         string  `json:"ledger_status"`
	ReconciliationStatus string  `json:"reconciliation_status"`
	Status               string  `json:"status"`
	JournalEntryID       *string `json:"journal_entry_id"`
	Message              string  `json:"message"`
	HTTPStatus           int     `json:"-"`
}

type ExternalTransferRequeryResult struct {
	TransferID           string    `json:"transfer_id"`
	AccountID            string    `json:"account_id"`
	Direction            string    `json:"direction"`
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
	Message              string    `json:"message"`
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

func (s *Service) ExternalInboundEvent(ctx context.Context, input ExternalInboundEventInput) (*ExternalInboundEventResult, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return nil, err
	}
	providerName := strings.TrimSpace(input.Provider)
	if providerName == "" {
		providerName = ProviderMockNIP
	}
	provider, err := s.provider(providerName)
	if err != nil {
		return nil, err
	}

	headers := inboundEventHeaders(input.Headers, institutionID)
	event, err := provider.ParseWebhook(ctx, input.Payload, headers)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, ErrInvalidRequest
	}
	if err := normalizeExternalInboundWebhookEvent(event, institutionID, providerName); err != nil {
		return nil, err
	}

	account, err := s.externalInboundDestinationAccount(ctx, *event)
	if errors.Is(err, ErrNotFound) {
		event.RequestFingerprint = externalInboundEventFingerprint(*event, "")
		return s.recordExternalInboundReview(ctx, *event, "", "unknown_destination", true)
	}
	if err != nil {
		return nil, err
	}
	event.AccountID = account.ID
	event.RequestFingerprint = externalInboundEventFingerprint(*event, account.ID)
	if externalInboundDestinationNeedsReview(*account) {
		return s.recordExternalInboundReview(ctx, *event, account.ID, "blocked_destination_account", true)
	}

	transfer, err := s.recordExternalInboundTransfer(ctx, *event, account.ID)
	if errors.Is(err, ErrConflict) {
		return s.recordExternalInboundReview(ctx, *event, account.ID, "provider_event_payload_conflict", false)
	}
	if errors.Is(err, ErrInvalidRequest) {
		current, getErr := s.repository.GetAccount(ctx, institutionID, account.ID)
		if getErr == nil && externalInboundDestinationNeedsReview(*current) {
			return s.recordExternalInboundReview(ctx, *event, account.ID, "blocked_destination_account", true)
		}
	}
	if err != nil {
		return nil, err
	}
	return externalInboundEventResultFromTransfer(*transfer, "event_recorded", 0), nil
}

func (s *Service) ExternalTransferRequery(ctx context.Context, input ExternalTransferRequeryInput) (*ExternalTransferRequeryResult, error) {
	normalized, err := normalizeExternalTransferRequeryInput(input)
	if err != nil {
		return nil, err
	}
	transfer, err := s.repository.GetTransfer(ctx, normalized.InstitutionID, normalized.TransferID)
	if err != nil {
		return nil, err
	}
	if transfer.Provider == ProviderLedgerInternal {
		return nil, ErrInvalidRequest
	}
	if transfer.Status != TransferStatusPending {
		return s.externalTransferRequeryResult(ctx, *transfer, "already_final")
	}
	if transfer.LedgerStatus != LedgerStatusPending || !requeryableProviderStatus(transfer.ProviderStatus) {
		return nil, ErrInvalidRequest
	}

	providerReference, err := requeryProviderReference(*transfer, normalized.ProviderReference)
	if err != nil {
		return nil, err
	}
	provider, err := s.provider(transfer.Provider)
	if err != nil {
		return nil, err
	}
	if err := applyMockRequeryScenario(provider, providerReference, normalized.Scenario); err != nil {
		return nil, err
	}

	providerResult, err := provider.RequeryTransfer(ctx, providerReference)
	if err != nil {
		if !providerRequeryStatusUnknown(err) {
			return nil, err
		}
		providerResult = providerUnknownRequeryResult(provider, *transfer, providerReference, normalized.Note)
	}
	if providerResult == nil {
		providerResult = providerUnknownRequeryResult(provider, *transfer, providerReference, normalized.Note)
	}

	clearing, err := s.repository.GetDefaultInternalSettlementAccount(ctx, transfer.InstitutionID, transfer.CurrencyID)
	if err != nil {
		return nil, err
	}
	completion, message, err := externalTransferRequeryCompletionInput(*transfer, clearing.ID, providerReference, normalized.Note, *providerResult)
	if err != nil {
		return nil, err
	}
	completed, err := s.repository.CompleteExternalTransferRequery(ctx, transfer.ID, completion)
	if err != nil {
		return nil, err
	}
	if completed.Status != TransferStatusPending {
		message = "already_final"
		switch completed.Status {
		case TransferStatusSucceeded:
			message = "requery_succeeded"
		case TransferStatusFailed:
			message = "requery_failed"
		}
	}
	return s.externalTransferRequeryResult(ctx, *completed, message)
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

func inboundEventHeaders(headers map[string]string, institutionID string) map[string]string {
	out := map[string]string{}
	for key, value := range headers {
		out[key] = value
	}
	out["X-Institution-ID"] = institutionID
	return out
}

func normalizeExternalInboundWebhookEvent(event *ProviderWebhookEvent, institutionID, providerName string) error {
	event.Provider = strings.TrimSpace(event.Provider)
	if event.Provider == "" {
		event.Provider = providerName
	}
	if event.Provider != providerName {
		return ErrUnsupportedProvider
	}
	if event.InstitutionID = strings.TrimSpace(event.InstitutionID); event.InstitutionID != "" && event.InstitutionID != institutionID {
		return ErrForbidden
	}
	event.InstitutionID = institutionID
	event.Direction = strings.ToLower(strings.TrimSpace(event.Direction))
	if event.Direction == "" {
		event.Direction = TransferDirectionInbound
	}
	if event.Direction != TransferDirectionInbound {
		return ErrInvalidRequest
	}
	event.Status = strings.ToLower(strings.TrimSpace(event.Status))
	event.CurrencyID = strings.ToUpper(strings.TrimSpace(event.CurrencyID))
	event.ProviderEventID = strings.TrimSpace(event.ProviderEventID)
	event.ProviderReference = strings.TrimSpace(event.ProviderReference)
	event.AccountID = strings.TrimSpace(event.AccountID)
	event.DestinationAccountNumber = strings.TrimSpace(event.DestinationAccountNumber)
	event.FailureReason = strings.TrimSpace(event.FailureReason)
	event.Narration = strings.TrimSpace(event.Narration)
	if event.CurrencyID == "" {
		event.CurrencyID = "NGN"
	}
	if event.Narration == "" {
		event.Narration = "External inbound transfer"
	}
	if event.ProviderEventID == "" || event.ProviderReference == "" || event.AmountMinor <= 0 || event.CurrencyID != "NGN" || !validTransferStatus(event.Status) {
		return ErrInvalidRequest
	}
	if event.AccountID == "" && event.DestinationAccountNumber == "" {
		return ErrInvalidRequest
	}
	if event.AccountID != "" {
		if _, err := uuid.Parse(event.AccountID); err != nil {
			return ErrInvalidRequest
		}
	}
	if event.DestinationAccountNumber != "" && !isTenDigitAccountNumber(event.DestinationAccountNumber) {
		return ErrInvalidRequest
	}
	return nil
}

func (s *Service) externalInboundDestinationAccount(ctx context.Context, event ProviderWebhookEvent) (*Account, error) {
	if event.AccountID != "" {
		return s.repository.GetAccount(ctx, event.InstitutionID, event.AccountID)
	}
	return s.repository.GetAccountByNumber(ctx, event.InstitutionID, event.DestinationAccountNumber)
}

func externalInboundDestinationNeedsReview(account Account) bool {
	return account.Kind == AccountKindCustomer &&
		(account.Status == AccountStatusFrozen || account.Status == AccountStatusClosed)
}

func (s *Service) recordExternalInboundTransfer(ctx context.Context, event ProviderWebhookEvent, accountID string) (*Transfer, error) {
	clearing, err := s.repository.GetDefaultInternalSettlementAccount(ctx, event.InstitutionID, event.CurrencyID)
	if err != nil {
		return nil, err
	}
	return s.repository.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:      event.InstitutionID,
		AccountID:          accountID,
		ClearingAccountID:  clearing.ID,
		Direction:          TransferDirectionInbound,
		Status:             event.Status,
		AmountMinor:        event.AmountMinor,
		CurrencyID:         event.CurrencyID,
		IdempotencyKey:     externalInboundEventIdempotencyKey(event),
		Provider:           event.Provider,
		ProviderReference:  event.ProviderReference,
		ProviderEventID:    event.ProviderEventID,
		ProviderStatus:     event.Status,
		RequestFingerprint: event.RequestFingerprint,
		FailureReason:      event.FailureReason,
		Narration:          event.Narration,
	})
}

func (s *Service) recordExternalInboundReview(ctx context.Context, event ProviderWebhookEvent, accountID, reason string, reserveProviderEvent bool) (*ExternalInboundEventResult, error) {
	reviewAccountID := strings.TrimSpace(accountID)
	if reviewAccountID == "" {
		clearing, err := s.repository.GetDefaultInternalSettlementAccount(ctx, event.InstitutionID, event.CurrencyID)
		if err != nil {
			return nil, err
		}
		reviewAccountID = clearing.ID
	}
	reviewStatus := TransferStatusFailed
	if event.Status == TransferStatusPending {
		reviewStatus = TransferStatusPending
	}
	transfer, err := s.repository.RecordProviderEventReview(ctx, RecordProviderEventReviewInput{
		InstitutionID:        event.InstitutionID,
		AccountID:            reviewAccountID,
		Direction:            TransferDirectionInbound,
		Status:               reviewStatus,
		ProviderStatus:       event.Status,
		AmountMinor:          event.AmountMinor,
		CurrencyID:           event.CurrencyID,
		IdempotencyKey:       externalInboundReviewIdempotencyKey(event),
		Provider:             event.Provider,
		ProviderReference:    event.ProviderReference,
		ProviderEventID:      event.ProviderEventID,
		RequestFingerprint:   event.RequestFingerprint,
		FailureReason:        reason,
		Narration:            firstNonBlank(event.Narration, "External inbound event manual review"),
		ReserveProviderEvent: reserveProviderEvent,
	})
	if err != nil {
		return nil, err
	}
	statusCode := 0
	if reason == "provider_event_payload_conflict" {
		statusCode = 409
	}
	return externalInboundEventResultFromTransfer(*transfer, reason, statusCode), nil
}

func externalInboundEventIdempotencyKey(event ProviderWebhookEvent) string {
	return "external-inbound:" + event.Provider + ":" + event.ProviderEventID
}

func externalInboundReviewIdempotencyKey(event ProviderWebhookEvent) string {
	return "external-inbound-review:" + event.Provider + ":" + event.ProviderEventID + ":" + event.RequestFingerprint
}

func externalInboundEventResultFromTransfer(transfer Transfer, message string, statusCode int) *ExternalInboundEventResult {
	transferID := transfer.ID
	providerEventID := ""
	if transfer.ProviderEventID != nil {
		providerEventID = *transfer.ProviderEventID
	}
	return &ExternalInboundEventResult{
		TransferID:           &transferID,
		ProviderEventID:      providerEventID,
		ProviderReference:    transfer.ProviderReference,
		ProviderStatus:       transfer.ProviderStatus,
		LedgerStatus:         transfer.LedgerStatus,
		ReconciliationStatus: transfer.ReconciliationStatus,
		Status:               transfer.Status,
		JournalEntryID:       transfer.JournalEntryID,
		Message:              message,
		HTTPStatus:           statusCode,
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

func normalizeExternalTransferRequeryInput(input ExternalTransferRequeryInput) (ExternalTransferRequeryInput, error) {
	institutionID, err := requireInstitutionID(input.InstitutionID)
	if err != nil {
		return ExternalTransferRequeryInput{}, err
	}
	input.InstitutionID = institutionID
	input.TransferID = strings.TrimSpace(input.TransferID)
	input.ProviderReference = strings.TrimSpace(input.ProviderReference)
	input.Scenario = strings.ToLower(strings.TrimSpace(input.Scenario))
	input.Note = strings.TrimSpace(input.Note)
	if _, err := uuid.Parse(input.TransferID); err != nil {
		return ExternalTransferRequeryInput{}, ErrInvalidRequest
	}
	if !validExternalRequeryScenario(input.Scenario) {
		return ExternalTransferRequeryInput{}, ErrInvalidRequest
	}
	return input, nil
}

func validExternalRequeryScenario(scenario string) bool {
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

func requeryProviderReference(transfer Transfer, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	current := strings.TrimSpace(transfer.ProviderReference)
	if requested != "" && current != "" && requested != current {
		return "", ErrConflict
	}
	providerReference := firstNonBlank(requested, current)
	if providerReference == "" {
		return "", ErrInvalidRequest
	}
	return providerReference, nil
}

type mockRequeryScenarioSetter interface {
	SetRequeryScenario(providerReference, scenario string) error
}

func applyMockRequeryScenario(provider TransferProvider, providerReference, scenario string) error {
	if scenario == "" {
		return nil
	}
	setter, ok := provider.(mockRequeryScenarioSetter)
	if !ok {
		return ErrInvalidRequest
	}
	return setter.SetRequeryScenario(providerReference, scenario)
}

func providerRequeryStatusUnknown(err error) bool {
	return providerTransferStatusUnknown(err) || errors.Is(err, ErrProviderUnavailable)
}

func providerUnknownRequeryResult(provider TransferProvider, transfer Transfer, providerReference, narration string) *ProviderTransferResult {
	return providerUnknownTransferResult(provider, ProviderTransferRequest{
		InstitutionID:     transfer.InstitutionID,
		AccountID:         transfer.AccountID,
		AmountMinor:       transfer.AmountMinor,
		CurrencyID:        transfer.CurrencyID,
		IdempotencyKey:    transfer.IdempotencyKey,
		ProviderReference: providerReference,
		Narration:         narration,
	})
}

func externalTransferRequeryCompletionInput(transfer Transfer, clearingAccountID, providerReference, note string, result ProviderTransferResult) (RecordTransferInput, string, error) {
	if strings.TrimSpace(result.Provider) != "" && strings.TrimSpace(result.Provider) != transfer.Provider {
		return RecordTransferInput{}, "", ErrConflict
	}
	resultReference := strings.TrimSpace(result.ProviderReference)
	if resultReference != "" && resultReference != providerReference {
		return RecordTransferInput{}, "", ErrConflict
	}
	status := strings.ToLower(strings.TrimSpace(result.Status))
	providerStatus := strings.ToLower(strings.TrimSpace(result.ProviderStatus))
	if status != "" && providerStatus != "" && providerStatus != status && providerStatus != TransferProviderStatusUnknown {
		return RecordTransferInput{}, "", ErrConflict
	}
	if providerStatus == "" {
		providerStatus = status
	}
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	if status == "" {
		status = providerStatus
	}
	if providerStatus == "" {
		providerStatus = status
	}
	if !validTransferStatus(status) || !validProviderStatus(providerStatus) {
		return RecordTransferInput{}, "", ErrInvalidRequest
	}

	failureReason := strings.TrimSpace(result.FailureReason)
	if providerStatus == TransferProviderStatusUnknown && failureReason == "" {
		failureReason = providerUnknownFailureReason
	}
	if status == TransferStatusFailed && failureReason == "" {
		failureReason = "provider_failed"
	}
	message := "requery_pending"
	switch {
	case providerStatus == TransferProviderStatusUnknown:
		message = "requery_provider_unknown"
	case status == TransferStatusSucceeded:
		message = "requery_succeeded"
	case status == TransferStatusFailed:
		message = "requery_failed"
	}
	return RecordTransferInput{
		InstitutionID:      transfer.InstitutionID,
		AccountID:          transfer.AccountID,
		ClearingAccountID:  clearingAccountID,
		Direction:          transfer.Direction,
		Status:             status,
		AmountMinor:        transfer.AmountMinor,
		CurrencyID:         transfer.CurrencyID,
		IdempotencyKey:     transfer.IdempotencyKey,
		Provider:           transfer.Provider,
		ProviderReference:  providerReference,
		ProviderStatus:     providerStatus,
		RequestFingerprint: transfer.RequestFingerprint,
		FailureReason:      failureReason,
		Narration:          firstNonBlank(note, result.Narration, transfer.Narration),
	}, message, nil
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

func (s *Service) externalTransferRequeryResult(ctx context.Context, transfer Transfer, message string) (*ExternalTransferRequeryResult, error) {
	hold, err := s.repository.GetTransferHold(ctx, transfer.InstitutionID, transfer.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var holdID *string
	if err == nil {
		holdID = &hold.ID
	}
	return &ExternalTransferRequeryResult{
		TransferID:           transfer.ID,
		AccountID:            transfer.AccountID,
		Direction:            transfer.Direction,
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
		Message:              message,
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
