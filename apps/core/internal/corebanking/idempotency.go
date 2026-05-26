package corebanking

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func transferRequestFingerprint(input RecordTransferInput) string {
	if fingerprint := strings.TrimSpace(input.RequestFingerprint); fingerprint != "" {
		return fingerprint
	}
	return fingerprintValues(
		"record_transfer",
		input.InstitutionID,
		input.Provider,
		input.Direction,
		input.Status,
		input.AccountID,
		input.ClearingAccountID,
		strconv.FormatInt(input.AmountMinor, 10),
		input.CurrencyID,
		input.ProviderReference,
		input.ProviderEventID,
		input.ProviderStatus,
		input.ReversalOfTransferID,
		strconv.FormatBool(input.RejectInsufficient),
		strconv.FormatBool(input.RequireAvailable),
	)
}

func transferRequestFingerprintMatches(transfer *Transfer, requestFingerprint string) bool {
	return transfer != nil &&
		strings.TrimSpace(transfer.RequestFingerprint) != "" &&
		strings.TrimSpace(transfer.RequestFingerprint) == strings.TrimSpace(requestFingerprint)
}

func mockTransferRequestFingerprint(direction string, req TransferRequest) string {
	return fingerprintValues(
		"mock_transfer",
		direction,
		req.InstitutionID,
		req.AccountID,
		strconv.FormatInt(req.AmountMinor, 10),
		req.CurrencyID,
		req.IdempotencyKey,
		req.ProviderReference,
		req.ProviderEventID,
		req.Status,
		req.Scenario,
		strconv.FormatInt(req.DelaySeconds, 10),
	)
}

func externalOutboundTransferRequestFingerprint(input ExternalOutboundTransferInput) string {
	return fingerprintValues(
		"external_outbound_transfer",
		input.InstitutionID,
		input.SourceAccountID,
		input.DestinationInstitutionCode,
		input.DestinationAccountNumber,
		input.DestinationAccountName,
		strconv.FormatInt(input.AmountMinor, 10),
		input.CurrencyID,
		input.IdempotencyKey,
		input.Provider,
		input.Reference,
		input.Narration,
		input.Scenario,
	)
}

func externalInboundEventFingerprint(event ProviderWebhookEvent, accountID string) string {
	return fingerprintValues(
		"external_inbound_event",
		event.Provider,
		event.ProviderEventID,
		event.ProviderReference,
		accountID,
		event.DestinationAccountNumber,
		strconv.FormatInt(event.AmountMinor, 10),
		event.CurrencyID,
		event.Status,
		TransferDirectionInbound,
	)
}

func providerWebhookRequestFingerprint(event ProviderWebhookEvent, originalProviderReference string, delaySeconds int64) string {
	return fingerprintValues(
		"provider_webhook",
		event.Provider,
		event.InstitutionID,
		event.AccountID,
		event.Direction,
		event.Status,
		strconv.FormatInt(event.AmountMinor, 10),
		event.CurrencyID,
		originalProviderReference,
		event.ProviderEventID,
		event.ReversalOfTransferID,
		event.FailureReason,
		event.Scenario,
		strconv.FormatInt(delaySeconds, 10),
	)
}

func fingerprintValues(values ...string) string {
	normalized := make([]string, len(values))
	for i, value := range values {
		normalized[i] = strings.TrimSpace(value)
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x1f")))
	return "v1:" + hex.EncodeToString(sum[:])
}
