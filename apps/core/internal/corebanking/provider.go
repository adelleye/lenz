package corebanking

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrProviderStatusUnknown = errors.New("provider transfer status unknown")
)

const providerUnknownFailureReason = "provider_status_unknown"

type Provider interface {
	Name() string
}

type TransferProvider interface {
	Provider
	NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error)
	InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error)
	RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error)
	ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error)
}

type NameEnquiryRequest struct {
	InstitutionID string
	BankCode      string
	AccountNumber string
}

type NameEnquiryResult struct {
	Provider          string
	ProviderReference string
	AccountName       string
	BankCode          string
	AccountNumber     string
}

type ProviderTransferRequest struct {
	InstitutionID     string
	AccountID         string
	AmountMinor       int64
	CurrencyID        string
	IdempotencyKey    string
	ProviderReference string
	ProviderEventID   string
	Status            string
	FailureReason     string
	Narration         string
	Scenario          string
	DelaySeconds      int64
}

type ProviderTransferResult struct {
	Provider          string
	ProviderReference string
	ProviderEventID   string
	Status            string
	ProviderStatus    string
	FailureReason     string
	Narration         string
	Delayed           bool
	DelayedUntil      *time.Time
}

type ProviderWebhookEvent struct {
	Provider             string
	InstitutionID        string
	AccountID            string
	Direction            string
	Status               string
	AmountMinor          int64
	CurrencyID           string
	IdempotencyKey       string
	ProviderReference    string
	ProviderEventID      string
	ReversalOfTransferID string
	FailureReason        string
	Narration            string
	Scenario             string
	RequestFingerprint   string
	Delayed              bool
	DelayedUntil         *time.Time
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
