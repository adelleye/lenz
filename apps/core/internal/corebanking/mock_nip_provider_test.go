package corebanking

import (
	"context"
	"testing"
	"time"
)

func TestMockNIPProviderNameEnquiry(t *testing.T) {
	provider := NewMockNIPProvider()

	result, err := provider.NameEnquiry(context.Background(), NameEnquiryRequest{
		BankCode:      "999001",
		AccountNumber: "9990000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Provider != ProviderMockNIP || result.AccountName != "Ada Demo Wallet" || result.ProviderReference == "" {
		t.Fatalf("name enquiry mismatch: %+v", result)
	}
}

func TestMockNIPProviderInitiateTransferScenarios(t *testing.T) {
	provider := NewMockNIPProvider()
	tests := []struct {
		name          string
		status        string
		scenario      string
		wantStatus    string
		wantFailure   bool
		wantReference bool
	}{
		{name: "successful outbound transfer", wantStatus: TransferStatusSucceeded, wantReference: true},
		{name: "pending outbound transfer", status: TransferStatusPending, wantStatus: TransferStatusPending, wantReference: true},
		{name: "failed outbound transfer", status: TransferStatusFailed, wantStatus: TransferStatusFailed, wantFailure: true, wantReference: true},
		{name: "scenario pending outbound transfer", scenario: MockProviderScenarioPending, wantStatus: TransferStatusPending, wantReference: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := provider.InitiateTransfer(context.Background(), ProviderTransferRequest{
				InstitutionID:  DemoInstitutionID,
				AccountID:      DemoCustomerAccountID,
				AmountMinor:    10000,
				CurrencyID:     "NGN",
				IdempotencyKey: "mock-nip-" + tt.name,
				Status:         tt.status,
				Scenario:       tt.scenario,
				Narration:      tt.name,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("status mismatch: got %s want %s", result.Status, tt.wantStatus)
			}
			if tt.wantFailure && result.FailureReason == "" {
				t.Fatalf("expected failure reason: %+v", result)
			}
			if tt.wantReference && result.ProviderReference == "" {
				t.Fatalf("expected generated provider reference: %+v", result)
			}
		})
	}
}

func TestMockNIPProviderDuplicateWebhookKeepsProviderEventIdentity(t *testing.T) {
	provider := NewMockNIPProvider()
	payload := []byte(`{
		"account_id":"44444444-4444-4444-4444-444444444444",
		"amount_minor":10000,
		"provider_event_id":"mock-duplicate-event",
		"provider_reference":"mock-duplicate-ref",
		"scenario":"duplicate"
	}`)
	headers := map[string]string{"Idempotency-Key": "mock-duplicate-idem"}

	first, err := provider.ParseWebhook(context.Background(), payload, headers)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.ParseWebhook(context.Background(), payload, headers)
	if err != nil {
		t.Fatal(err)
	}

	if first.ProviderEventID != second.ProviderEventID || first.ProviderReference != second.ProviderReference {
		t.Fatalf("duplicate webhook identity changed: first=%+v second=%+v", first, second)
	}
}

func TestMockNIPProviderDelayedWebhookSimulation(t *testing.T) {
	now := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	provider := NewMockNIPProvider(WithMockNIPClock(func() time.Time { return now }))
	payload := []byte(`{
		"account_id":"44444444-4444-4444-4444-444444444444",
		"amount_minor":10000,
		"provider_event_id":"mock-delayed-event",
		"scenario":"delayed",
		"delay_seconds":30
	}`)

	event, err := provider.ParseWebhook(context.Background(), payload, map[string]string{"Idempotency-Key": "mock-delayed-idem"})
	if err != nil {
		t.Fatal(err)
	}

	if !event.Delayed || event.DelayedUntil == nil || !event.DelayedUntil.Equal(now.Add(30*time.Second)) || event.Status != TransferStatusPending {
		t.Fatalf("delayed event mismatch: %+v", event)
	}
}

func TestMockNIPProviderReversalWebhookSimulation(t *testing.T) {
	provider := NewMockNIPProvider()
	payload := []byte(`{
		"provider_event_id":"mock-reversal-event",
		"provider_reference":"mock-reversal-ref",
		"scenario":"reversal",
		"reversal_of_transfer_id":"original-transfer-id"
	}`)

	event, err := provider.ParseWebhook(context.Background(), payload, map[string]string{"Idempotency-Key": "mock-reversal-idem"})
	if err != nil {
		t.Fatal(err)
	}

	if event.Direction != TransferDirectionReversal || event.ReversalOfTransferID != "original-transfer-id" || event.Status != TransferStatusSucceeded {
		t.Fatalf("reversal event mismatch: %+v", event)
	}
}
