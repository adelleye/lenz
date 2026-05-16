package corebanking

import (
	"context"
	"errors"
	"testing"
)

func TestHybridProviderPrimaryTransferSuccess(t *testing.T) {
	primary := &hybridTestProvider{
		name: "primary",
		initiateResult: &ProviderTransferResult{
			ProviderReference: "primary-ref",
			Status:            TransferStatusSucceeded,
		},
	}
	fallback := &hybridTestProvider{name: "fallback"}
	provider := NewHybridProvider("hybrid", primary, fallback)

	result, err := provider.InitiateTransfer(context.Background(), validProviderTransferRequest("primary-success"))
	if err != nil {
		t.Fatal(err)
	}

	if result.Provider != "primary" || result.ProviderReference != "primary-ref" || result.ProviderStatus != TransferStatusSucceeded {
		t.Fatalf("primary result mismatch: %+v", result)
	}
	if primary.initiateCalls != 1 || fallback.initiateCalls != 0 {
		t.Fatalf("unexpected initiate calls: primary=%d fallback=%d", primary.initiateCalls, fallback.initiateCalls)
	}
}

func TestHybridProviderNameEnquiryFallsBackWhenPrimaryFails(t *testing.T) {
	primary := &hybridTestProvider{name: "primary", nameEnquiryErr: ErrNotFound}
	fallback := &hybridTestProvider{
		name: "fallback",
		nameEnquiryResult: &NameEnquiryResult{
			AccountName:   "Fallback Account",
			BankCode:      "999001",
			AccountNumber: "9990000001",
		},
	}
	provider := NewHybridProvider("hybrid", primary, fallback)

	result, err := provider.NameEnquiry(context.Background(), NameEnquiryRequest{BankCode: "999001", AccountNumber: "9990000001"})
	if err != nil {
		t.Fatal(err)
	}

	if result.Provider != "fallback" || result.AccountName != "Fallback Account" {
		t.Fatalf("fallback name enquiry mismatch: %+v", result)
	}
	if primary.nameEnquiryCalls != 1 || fallback.nameEnquiryCalls != 1 {
		t.Fatalf("unexpected name enquiry calls: primary=%d fallback=%d", primary.nameEnquiryCalls, fallback.nameEnquiryCalls)
	}
}

func TestHybridProviderTransferDoesNotFallbackOnTimeoutOrUnknownStatus(t *testing.T) {
	primary := &hybridTestProvider{name: "primary", initiateErr: context.DeadlineExceeded}
	fallback := &hybridTestProvider{
		name: "fallback",
		initiateResult: &ProviderTransferResult{
			ProviderReference: "fallback-ref",
			Status:            TransferStatusSucceeded,
		},
	}
	provider := NewHybridProvider("hybrid", primary, fallback)

	result, err := provider.InitiateTransfer(context.Background(), validProviderTransferRequest("unknown-status"))
	if err != nil {
		t.Fatal(err)
	}

	if result.Provider != "primary" || result.Status != TransferStatusPending || result.ProviderStatus != TransferProviderStatusUnknown {
		t.Fatalf("unknown status result mismatch: %+v", result)
	}
	if primary.initiateCalls != 1 || fallback.initiateCalls != 0 {
		t.Fatalf("fallback should not run after unknown status: primary=%d fallback=%d", primary.initiateCalls, fallback.initiateCalls)
	}
}

func TestHybridProviderTransferFallbackRequiresPreSubmissionFailure(t *testing.T) {
	primary := &hybridTestProvider{name: "primary", initiateErr: errors.Join(ErrProviderPreSubmissionFailure, errors.New("dial failed"))}
	fallback := &hybridTestProvider{
		name: "fallback",
		initiateResult: &ProviderTransferResult{
			ProviderReference: "fallback-ref",
			Status:            TransferStatusSucceeded,
		},
	}
	provider := NewHybridProvider("hybrid", primary, fallback)

	result, err := provider.InitiateTransfer(context.Background(), validProviderTransferRequest("pre-submit-fallback"))
	if err != nil {
		t.Fatal(err)
	}

	if result.Provider != "fallback" || result.ProviderReference != "fallback-ref" || result.ProviderStatus != TransferStatusSucceeded {
		t.Fatalf("fallback transfer mismatch: %+v", result)
	}
	if primary.initiateCalls != 1 || fallback.initiateCalls != 1 {
		t.Fatalf("unexpected initiate calls: primary=%d fallback=%d", primary.initiateCalls, fallback.initiateCalls)
	}
}

func TestHybridProviderRequeryUsesOriginalTransferProvider(t *testing.T) {
	primary := &hybridTestProvider{name: "primary", initiateErr: errors.Join(ErrProviderPreSubmissionFailure, errors.New("dial failed"))}
	fallback := &hybridTestProvider{
		name: "fallback",
		initiateResult: &ProviderTransferResult{
			ProviderReference: "fallback-ref",
			Status:            TransferStatusPending,
		},
		requeryResult: &ProviderTransferResult{
			ProviderReference: "fallback-ref",
			Status:            TransferStatusSucceeded,
		},
	}
	provider := NewHybridProvider("hybrid", primary, fallback)

	if _, err := provider.InitiateTransfer(context.Background(), validProviderTransferRequest("requery-original")); err != nil {
		t.Fatal(err)
	}
	result, err := provider.RequeryTransfer(context.Background(), "fallback-ref")
	if err != nil {
		t.Fatal(err)
	}

	if result.Provider != "fallback" || result.Status != TransferStatusSucceeded {
		t.Fatalf("requery result mismatch: %+v", result)
	}
	if primary.requeryCalls != 0 || fallback.requeryCalls != 1 {
		t.Fatalf("requery used wrong provider: primary=%d fallback=%d", primary.requeryCalls, fallback.requeryCalls)
	}
}

func validProviderTransferRequest(idempotencyKey string) ProviderTransferRequest {
	return ProviderTransferRequest{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       1000,
		CurrencyID:        "NGN",
		IdempotencyKey:    idempotencyKey,
		ProviderReference: idempotencyKey + "-ref",
		Narration:         "test transfer",
	}
}

type hybridTestProvider struct {
	name string

	nameEnquiryCalls int
	initiateCalls    int
	requeryCalls     int

	nameEnquiryResult *NameEnquiryResult
	nameEnquiryErr    error
	initiateResult    *ProviderTransferResult
	initiateErr       error
	requeryResult     *ProviderTransferResult
	requeryErr        error
}

func (p *hybridTestProvider) Name() string {
	return p.name
}

func (p *hybridTestProvider) NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error) {
	p.nameEnquiryCalls++
	if p.nameEnquiryErr != nil {
		return nil, p.nameEnquiryErr
	}
	return copyOf(*p.nameEnquiryResult), nil
}

func (p *hybridTestProvider) InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error) {
	p.initiateCalls++
	if p.initiateErr != nil {
		return nil, p.initiateErr
	}
	return copyOf(*p.initiateResult), nil
}

func (p *hybridTestProvider) RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error) {
	p.requeryCalls++
	if p.requeryErr != nil {
		return nil, p.requeryErr
	}
	return copyOf(*p.requeryResult), nil
}

func (p *hybridTestProvider) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error) {
	return nil, ErrInvalidRequest
}
