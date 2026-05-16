package corebanking

import (
	"context"
	"time"
)

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
	Delayed              bool
	DelayedUntil         *time.Time
}
