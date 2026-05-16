package corebanking

import (
	"context"
	"errors"
	"strings"
	"sync"
)

const providerUnknownFailureReason = "provider_status_unknown"

type HybridProvider struct {
	name     string
	primary  Provider
	fallback Provider

	mu                 sync.Mutex
	providerByTransfer map[string]TransferProvider
}

var _ Provider = (*HybridProvider)(nil)
var _ TransferProvider = (*HybridProvider)(nil)

func NewHybridProvider(name string, primary Provider, fallback Provider) *HybridProvider {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "hybrid"
	}
	return &HybridProvider{
		name:               name,
		primary:            primary,
		fallback:           fallback,
		providerByTransfer: map[string]TransferProvider{},
	}
}

func (p *HybridProvider) Name() string {
	return p.name
}

func (p *HybridProvider) NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error) {
	primary, err := transferCapability(p.primary)
	if err != nil {
		return nil, err
	}
	result, err := primary.NameEnquiry(ctx, request)
	if err == nil {
		return normalizeNameEnquiryResult(primary, result), nil
	}

	fallback, fallbackErr := transferCapability(p.fallback)
	if fallbackErr != nil {
		return nil, err
	}
	result, err = fallback.NameEnquiry(ctx, request)
	if err != nil {
		return nil, err
	}
	return normalizeNameEnquiryResult(fallback, result), nil
}

func (p *HybridProvider) InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error) {
	primary, err := transferCapability(p.primary)
	if err != nil {
		return nil, err
	}
	result, err := primary.InitiateTransfer(ctx, request)
	if err == nil {
		return p.rememberTransferProvider(primary, normalizeProviderTransferResult(primary, result)), nil
	}
	if !providerTransferDefinitelyNotSubmitted(err) {
		return p.rememberTransferProvider(primary, providerUnknownTransferResult(primary, request)), nil
	}

	fallback, fallbackErr := transferCapability(p.fallback)
	if fallbackErr != nil {
		return nil, err
	}
	result, err = fallback.InitiateTransfer(ctx, request)
	if err == nil {
		return p.rememberTransferProvider(fallback, normalizeProviderTransferResult(fallback, result)), nil
	}
	if !providerTransferDefinitelyNotSubmitted(err) {
		return p.rememberTransferProvider(fallback, providerUnknownTransferResult(fallback, request)), nil
	}
	return nil, err
}

func (p *HybridProvider) RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error) {
	providerReference = strings.TrimSpace(providerReference)
	if providerReference == "" {
		return nil, ErrInvalidRequest
	}

	provider := p.providerForTransfer(providerReference)
	if provider == nil {
		return nil, ErrNotFound
	}
	result, err := provider.RequeryTransfer(ctx, providerReference)
	if err != nil {
		return nil, err
	}
	return normalizeProviderTransferResult(provider, result), nil
}

func (p *HybridProvider) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error) {
	primary, err := transferCapability(p.primary)
	if err != nil {
		return nil, err
	}
	return primary.ParseWebhook(ctx, payload, headers)
}

func (p *HybridProvider) rememberTransferProvider(provider TransferProvider, result *ProviderTransferResult) *ProviderTransferResult {
	if result == nil {
		return nil
	}
	providerReference := strings.TrimSpace(result.ProviderReference)
	if providerReference == "" {
		return result
	}
	p.mu.Lock()
	p.providerByTransfer[providerReference] = provider
	p.mu.Unlock()
	return result
}

func (p *HybridProvider) providerForTransfer(providerReference string) TransferProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.providerByTransfer[providerReference]
}

func transferCapability(provider Provider) (TransferProvider, error) {
	if provider == nil {
		return nil, ErrProviderCapabilityUnavailable
	}
	transferProvider, ok := provider.(TransferProvider)
	if !ok {
		return nil, ErrProviderCapabilityUnavailable
	}
	return transferProvider, nil
}

func normalizeNameEnquiryResult(provider TransferProvider, result *NameEnquiryResult) *NameEnquiryResult {
	if result == nil {
		return nil
	}
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = provider.Name()
	}
	return result
}

func normalizeProviderTransferResult(provider TransferProvider, result *ProviderTransferResult) *ProviderTransferResult {
	if result == nil {
		return nil
	}
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = provider.Name()
	}
	result.Status = strings.ToLower(strings.TrimSpace(result.Status))
	result.ProviderStatus = strings.ToLower(strings.TrimSpace(result.ProviderStatus))
	if result.Status == TransferProviderStatusUnknown {
		result.ProviderStatus = TransferProviderStatusUnknown
	}
	if result.ProviderStatus == TransferProviderStatusUnknown {
		result.Status = TransferStatusPending
	}
	if result.ProviderStatus == "" {
		result.ProviderStatus = result.Status
	}
	result.ProviderReference = strings.TrimSpace(result.ProviderReference)
	result.ProviderEventID = strings.TrimSpace(result.ProviderEventID)
	result.FailureReason = strings.TrimSpace(result.FailureReason)
	result.Narration = strings.TrimSpace(result.Narration)
	return result
}

func providerUnknownTransferResult(provider TransferProvider, request ProviderTransferRequest) *ProviderTransferResult {
	return &ProviderTransferResult{
		Provider:          provider.Name(),
		ProviderReference: providerTransferReference(request),
		Status:            TransferStatusPending,
		ProviderStatus:    TransferProviderStatusUnknown,
		FailureReason:     providerUnknownFailureReason,
		Narration:         strings.TrimSpace(request.Narration),
	}
}

func providerTransferDefinitelyNotSubmitted(err error) bool {
	return errors.Is(err, ErrProviderPreSubmissionFailure)
}

func providerTransferStatusUnknown(err error) bool {
	return errors.Is(err, ErrProviderStatusUnknown) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

func providerTransferReference(request ProviderTransferRequest) string {
	if providerReference := strings.TrimSpace(request.ProviderReference); providerReference != "" {
		return providerReference
	}
	return strings.TrimSpace(request.IdempotencyKey)
}
