package corebanking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	MockProviderScenarioSuccess   = "success"
	MockProviderScenarioPending   = "pending"
	MockProviderScenarioFailed    = "failed"
	MockProviderScenarioDuplicate = "duplicate"
	MockProviderScenarioDelayed   = "delayed"
	MockProviderScenarioReversal  = "reversal"
)

type MockNIPProvider struct {
	mu        sync.Mutex
	clock     func() time.Time
	transfers map[string]ProviderTransferResult
}

type MockNIPOption func(*MockNIPProvider)

func NewMockNIPProvider(options ...MockNIPOption) *MockNIPProvider {
	provider := &MockNIPProvider{
		clock:     func() time.Time { return time.Now().UTC() },
		transfers: map[string]ProviderTransferResult{},
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

	accountName := "Mock NIP Account " + accountNumber
	if accountNumber == "9990000001" {
		accountName = "Ada Demo Wallet"
	}

	return &NameEnquiryResult{
		Provider:          p.Name(),
		ProviderReference: p.providerReference("mock-nip-ne", ""),
		AccountName:       accountName,
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

	status, failureReason, delayed, err := p.resolveScenario(request.Status, request.FailureReason, request.Scenario)
	if err != nil {
		return nil, err
	}
	result := ProviderTransferResult{
		Provider:          p.Name(),
		ProviderReference: p.providerReference("mock-nip-out", request.ProviderReference),
		ProviderEventID:   strings.TrimSpace(request.ProviderEventID),
		Status:            status,
		FailureReason:     failureReason,
		Narration:         strings.TrimSpace(request.Narration),
		Delayed:           delayed,
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

func (p *MockNIPProvider) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var request mockWebhookPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, ErrInvalidRequest
	}

	event := ProviderWebhookEvent{
		Provider:             p.Name(),
		InstitutionID:        firstNonBlank(request.InstitutionID, headerValue(headers, "X-Institution-ID"), DemoInstitutionID),
		AccountID:            strings.TrimSpace(request.AccountID),
		Direction:            strings.ToLower(strings.TrimSpace(request.Direction)),
		AmountMinor:          request.AmountMinor,
		CurrencyID:           strings.ToUpper(strings.TrimSpace(request.CurrencyID)),
		IdempotencyKey:       firstNonBlank(request.IdempotencyKey, headerValue(headers, "Idempotency-Key")),
		ProviderReference:    p.providerReference("mock-nip-in", request.ProviderReference),
		ProviderEventID:      strings.TrimSpace(request.ProviderEventID),
		ReversalOfTransferID: strings.TrimSpace(request.ReversalOfTransferID),
		FailureReason:        strings.TrimSpace(request.FailureReason),
		Narration:            strings.TrimSpace(request.Narration),
		Scenario:             strings.ToLower(strings.TrimSpace(request.Scenario)),
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
	if event.ProviderEventID == "" || event.IdempotencyKey == "" {
		return nil, ErrInvalidRequest
	}
	if event.Direction != TransferDirectionReversal && (event.AccountID == "" || event.AmountMinor <= 0) {
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
	if delayed {
		delayedUntil := p.clock().Add(delayDuration(request.DelaySeconds))
		event.DelayedUntil = &delayedUntil
	}
	return &event, nil
}

type mockWebhookPayload struct {
	InstitutionID        string `json:"institution_id"`
	AccountID            string `json:"account_id"`
	Direction            string `json:"direction"`
	Status               string `json:"status"`
	AmountMinor          int64  `json:"amount_minor"`
	CurrencyID           string `json:"currency_id"`
	IdempotencyKey       string `json:"idempotency_key"`
	ProviderReference    string `json:"provider_reference"`
	ProviderEventID      string `json:"provider_event_id"`
	ReversalOfTransferID string `json:"reversal_of_transfer_id"`
	FailureReason        string `json:"failure_reason"`
	Narration            string `json:"narration"`
	Scenario             string `json:"scenario"`
	DelaySeconds         int64  `json:"delay_seconds"`
}

func (p *MockNIPProvider) resolveScenario(requestedStatus, requestedFailureReason, scenario string) (string, string, bool, error) {
	status := strings.ToLower(strings.TrimSpace(requestedStatus))
	failureReason := strings.TrimSpace(requestedFailureReason)
	scenario = strings.ToLower(strings.TrimSpace(scenario))
	if scenario == "" {
		scenario = MockProviderScenarioSuccess
	}

	delayed := false
	switch scenario {
	case MockProviderScenarioSuccess, MockProviderScenarioDuplicate, MockProviderScenarioReversal:
		if status == "" {
			status = TransferStatusSucceeded
		}
	case MockProviderScenarioPending:
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

func (p *MockNIPProvider) providerReference(prefix, existing string) string {
	existing = strings.TrimSpace(existing)
	if existing != "" {
		return existing
	}
	return fmt.Sprintf("%s-%s", prefix, newID())
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
