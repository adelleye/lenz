package corebanking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	MockProviderScenarioSuccess         = "success"
	MockProviderScenarioPending         = "pending"
	MockProviderScenarioFailed          = "failed"
	MockProviderScenarioTimeout         = "timeout"
	MockProviderScenarioProviderUnknown = "provider_unknown"
	MockProviderScenarioDuplicate       = "duplicate"
	MockProviderScenarioDelayed         = "delayed"
	MockProviderScenarioReversal        = "reversal"
)

const (
	mockNIPDemoBankCode        = "999001"
	mockNIPUnavailableBankCode = "999998"
	mockNIPDemoAccountNumber   = "9990000001"
	mockNIPDemoAccountName     = "Ada Demo Wallet"
)

type MockNIPProvider struct {
	mu            sync.Mutex
	clock         func() time.Time
	transfers     map[string]ProviderTransferResult
	requeryErrors map[string]error
}

var _ TransferProvider = (*MockNIPProvider)(nil)

type MockNIPOption func(*MockNIPProvider)

func NewMockNIPProvider(options ...MockNIPOption) *MockNIPProvider {
	provider := &MockNIPProvider{
		clock:         func() time.Time { return time.Now().UTC() },
		transfers:     map[string]ProviderTransferResult{},
		requeryErrors: map[string]error{},
	}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func WithMockNIPClock(clock func() time.Time) MockNIPOption {
	return func(provider *MockNIPProvider) {
		if clock != nil {
			provider.clock = clock
		}
	}
}

func (p *MockNIPProvider) Name() string {
	return ProviderMockNIP
}

func (p *MockNIPProvider) NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bankCode := strings.TrimSpace(request.BankCode)
	accountNumber := strings.TrimSpace(request.AccountNumber)
	if bankCode == "" || accountNumber == "" {
		return nil, ErrInvalidRequest
	}
	if bankCode == mockNIPUnavailableBankCode {
		return nil, ErrProviderUnavailable
	}
	if bankCode != mockNIPDemoBankCode || accountNumber != mockNIPDemoAccountNumber {
		return nil, ErrNotFound
	}

	return &NameEnquiryResult{
		Provider:          p.Name(),
		ProviderReference: p.providerReference("mock-nip-ne", ""),
		AccountName:       mockNIPDemoAccountName,
		BankCode:          bankCode,
		AccountNumber:     accountNumber,
	}, nil
}

func (p *MockNIPProvider) InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.AccountID) == "" || request.AmountMinor <= 0 || strings.TrimSpace(request.IdempotencyKey) == "" {
		return nil, ErrInvalidRequest
	}
	scenario := mockScenario(request.Scenario)
	if scenario == MockProviderScenarioTimeout {
		return nil, ErrProviderStatusUnknown
	}

	status, failureReason, delayed, err := p.resolveScenario(request.Status, request.FailureReason, scenario)
	if err != nil {
		return nil, err
	}
	result := ProviderTransferResult{
		Provider:          p.Name(),
		ProviderReference: p.providerReference("mock-nip-out", request.ProviderReference),
		ProviderEventID:   strings.TrimSpace(request.ProviderEventID),
		Status:            status,
		ProviderStatus:    status,
		FailureReason:     failureReason,
		Narration:         strings.TrimSpace(request.Narration),
		Delayed:           delayed,
	}
	if scenario == MockProviderScenarioProviderUnknown {
		result.ProviderStatus = TransferProviderStatusUnknown
		result.FailureReason = providerUnknownFailureReason
	}
	if delayed {
		delayedUntil := p.clock().Add(delayDuration(request.DelaySeconds))
		result.DelayedUntil = &delayedUntil
	}

	p.mu.Lock()
	p.transfers[result.ProviderReference] = result
	p.mu.Unlock()
	return copyProviderTransferResult(result), nil
}

func (p *MockNIPProvider) RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	providerReference = strings.TrimSpace(providerReference)
	if providerReference == "" {
		return nil, ErrInvalidRequest
	}

	p.mu.Lock()
	if err := p.requeryErrors[providerReference]; err != nil {
		p.mu.Unlock()
		return nil, err
	}
	result, ok := p.transfers[providerReference]
	p.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	if result.Delayed && result.DelayedUntil != nil && !p.clock().Before(*result.DelayedUntil) {
		result.Delayed = false
		result.DelayedUntil = nil
		if result.Status == TransferStatusPending {
			result.Status = TransferStatusSucceeded
		}
	}
	return copyProviderTransferResult(result), nil
}

func (p *MockNIPProvider) SetRequeryScenario(providerReference, scenario string) error {
	providerReference = strings.TrimSpace(providerReference)
	scenario = mockScenario(scenario)
	if providerReference == "" || scenario == "" {
		return ErrInvalidRequest
	}
	if scenario == MockProviderScenarioTimeout {
		p.mu.Lock()
		if _, ok := p.transfers[providerReference]; !ok {
			p.mu.Unlock()
			return ErrNotFound
		}
		p.requeryErrors[providerReference] = ErrProviderStatusUnknown
		p.mu.Unlock()
		return nil
	}

	status, failureReason, delayed, err := p.resolveScenario("", "", scenario)
	if err != nil {
		return err
	}
	p.mu.Lock()
	result, ok := p.transfers[providerReference]
	if !ok {
		p.mu.Unlock()
		return ErrNotFound
	}
	result.Status = status
	result.ProviderStatus = status
	result.FailureReason = failureReason
	result.Delayed = delayed
	result.DelayedUntil = nil
	if scenario == MockProviderScenarioProviderUnknown {
		result.ProviderStatus = TransferProviderStatusUnknown
		result.FailureReason = providerUnknownFailureReason
	}
	if delayed {
		delayedUntil := p.clock().Add(delayDuration(0))
		result.DelayedUntil = &delayedUntil
	}
	p.transfers[providerReference] = result
	delete(p.requeryErrors, providerReference)
	p.mu.Unlock()
	return nil
}

func (p *MockNIPProvider) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var request mockWebhookPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, ErrInvalidRequest
	}

	event := ProviderWebhookEvent{
		Provider:                 firstNonBlank(request.Provider, p.Name()),
		InstitutionID:            firstNonBlank(request.InstitutionID, headerValue(headers, "X-Institution-ID"), DemoInstitutionID),
		AccountID:                strings.TrimSpace(request.AccountID),
		DestinationAccountNumber: strings.TrimSpace(request.DestinationAccountNumber),
		Direction:                strings.ToLower(strings.TrimSpace(request.Direction)),
		AmountMinor:              request.AmountMinor,
		CurrencyID:               strings.ToUpper(strings.TrimSpace(request.CurrencyID)),
		IdempotencyKey:           firstNonBlank(request.IdempotencyKey, headerValue(headers, "Idempotency-Key")),
		ProviderReference:        p.providerReference("mock-nip-in", request.ProviderReference),
		ProviderEventID:          strings.TrimSpace(request.ProviderEventID),
		ReversalOfTransferID:     strings.TrimSpace(request.ReversalOfTransferID),
		SenderName:               strings.TrimSpace(request.SenderName),
		SenderAccountNumber:      strings.TrimSpace(request.SenderAccountNumber),
		SenderInstitutionCode:    strings.TrimSpace(request.SenderInstitutionCode),
		FailureReason:            strings.TrimSpace(request.FailureReason),
		Narration:                strings.TrimSpace(request.Narration),
		Scenario:                 strings.ToLower(strings.TrimSpace(request.Scenario)),
	}
	if event.Direction == "" {
		event.Direction = TransferDirectionInbound
	}
	if event.Scenario == MockProviderScenarioReversal {
		event.Direction = TransferDirectionReversal
	}
	if event.CurrencyID == "" {
		event.CurrencyID = "NGN"
	}
	if event.ProviderEventID == "" {
		return nil, ErrInvalidRequest
	}
	if event.Direction != TransferDirectionReversal && event.AccountID == "" && event.DestinationAccountNumber == "" {
		return nil, ErrInvalidRequest
	}
	if event.Direction != TransferDirectionReversal && event.DestinationAccountNumber != "" && !isTenDigitAccountNumber(event.DestinationAccountNumber) {
		return nil, ErrInvalidRequest
	}
	if event.Direction != TransferDirectionReversal && event.AmountMinor <= 0 {
		return nil, ErrInvalidRequest
	}
	if event.Direction == TransferDirectionReversal && event.ReversalOfTransferID == "" {
		return nil, ErrInvalidRequest
	}

	status, failureReason, delayed, err := p.resolveScenario(request.Status, event.FailureReason, event.Scenario)
	if err != nil {
		return nil, err
	}
	event.Status = status
	event.FailureReason = failureReason
	event.Delayed = delayed
	if event.Narration == "" {
		event.Narration = "Mock NIP webhook"
	}
	event.RequestFingerprint = providerWebhookRequestFingerprint(event, strings.TrimSpace(request.ProviderReference), request.DelaySeconds)
	if delayed {
		delayedUntil := p.clock().Add(delayDuration(request.DelaySeconds))
		event.DelayedUntil = &delayedUntil
	}
	p.rememberWebhookEvent(event)
	return &event, nil
}

func (p *MockNIPProvider) rememberWebhookEvent(event ProviderWebhookEvent) {
	providerReference := strings.TrimSpace(event.ProviderReference)
	if providerReference == "" {
		return
	}
	result := ProviderTransferResult{
		Provider:          p.Name(),
		ProviderReference: providerReference,
		ProviderEventID:   strings.TrimSpace(event.ProviderEventID),
		Status:            event.Status,
		ProviderStatus:    event.Status,
		FailureReason:     strings.TrimSpace(event.FailureReason),
		Narration:         strings.TrimSpace(event.Narration),
		Delayed:           event.Delayed,
		DelayedUntil:      event.DelayedUntil,
	}
	p.mu.Lock()
	p.transfers[providerReference] = result
	delete(p.requeryErrors, providerReference)
	p.mu.Unlock()
}

type mockWebhookPayload struct {
	InstitutionID            string `json:"institution_id"`
	Provider                 string `json:"provider"`
	AccountID                string `json:"account_id"`
	DestinationAccountNumber string `json:"destination_account_number"`
	Direction                string `json:"direction"`
	Status                   string `json:"status"`
	AmountMinor              int64  `json:"amount_minor"`
	CurrencyID               string `json:"currency_id"`
	IdempotencyKey           string `json:"idempotency_key"`
	ProviderReference        string `json:"provider_reference"`
	ProviderEventID          string `json:"provider_event_id"`
	ReversalOfTransferID     string `json:"reversal_of_transfer_id"`
	SenderName               string `json:"sender_name"`
	SenderAccountNumber      string `json:"sender_account_number"`
	SenderInstitutionCode    string `json:"sender_institution_code"`
	FailureReason            string `json:"failure_reason"`
	Narration                string `json:"narration"`
	Scenario                 string `json:"scenario"`
	DelaySeconds             int64  `json:"delay_seconds"`
}

func (p *MockNIPProvider) resolveScenario(requestedStatus, requestedFailureReason, scenario string) (string, string, bool, error) {
	status := strings.ToLower(strings.TrimSpace(requestedStatus))
	failureReason := strings.TrimSpace(requestedFailureReason)
	scenario = mockScenario(scenario)
	if scenario == "" {
		scenario = MockProviderScenarioSuccess
	}

	delayed := false
	switch scenario {
	case MockProviderScenarioSuccess, MockProviderScenarioDuplicate, MockProviderScenarioReversal:
		if status == "" {
			status = TransferStatusSucceeded
		}
	case MockProviderScenarioPending, MockProviderScenarioProviderUnknown:
		status = TransferStatusPending
	case MockProviderScenarioFailed:
		status = TransferStatusFailed
	case MockProviderScenarioDelayed:
		delayed = true
		if status == "" {
			status = TransferStatusPending
		}
	default:
		return "", "", false, ErrInvalidRequest
	}

	switch status {
	case TransferStatusSucceeded:
		failureReason = ""
	case TransferStatusPending:
	case TransferStatusFailed:
		if failureReason == "" {
			failureReason = "mock_provider_failed"
		}
	default:
		return "", "", false, ErrInvalidRequest
	}
	return status, failureReason, delayed, nil
}

func mockScenario(scenario string) string {
	return strings.ToLower(strings.TrimSpace(scenario))
}

func (p *MockNIPProvider) providerReference(prefix, existing string) string {
	existing = strings.TrimSpace(existing)
	if existing != "" {
		return existing
	}
	return fmt.Sprintf("%s-%s", prefix, uuid.Must(uuid.NewRandom()).String())
}

func delayDuration(seconds int64) time.Duration {
	if seconds <= 0 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

func headerValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func copyProviderTransferResult(result ProviderTransferResult) *ProviderTransferResult {
	return &result
}
