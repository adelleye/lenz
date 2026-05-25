package corebanking

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	AuditActionCustomerCreated        = "customer.created"
	AuditActionAccountCreated         = "account.created"
	AuditActionInternalCreditPosted   = "internal_credit.posted"
	AuditActionInternalDebitPosted    = "internal_debit.posted"
	AuditActionInternalTransferPosted = "internal_transfer.posted"
	AuditActionAccountFrozen          = "account.frozen"
	AuditActionAccountUnfrozen        = "account.unfrozen"
	AuditActionPNDActivated           = "account.pnd_activated"
	AuditActionPNDDeactivated         = "account.pnd_deactivated"
	AuditActionLienPlaced             = "account.lien_placed"
	AuditActionLienReleased           = "account.lien_released"
)

type auditEventInput struct {
	InstitutionID  string
	Action         string
	EntityType     string
	EntityID       string
	CustomerID     string
	AccountID      string
	TransferID     string
	JournalEntryID string
	IdempotencyKey string
	Reference      string
	OldStatus      string
	NewStatus      string
	Metadata       map[string]string
	CreatedAt      time.Time
}

func newAuditEvent(input auditEventInput) (AuditEvent, string, error) {
	now := input.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	event := AuditEvent{
		ID:            uuid.Must(uuid.NewRandom()).String(),
		InstitutionID: strings.TrimSpace(input.InstitutionID),
		ActorType:     "system",
		ActorID:       "system",
		RequestID:     "service",
		Action:        strings.TrimSpace(input.Action),
		EntityType:    strings.TrimSpace(input.EntityType),
		EntityID:      strings.TrimSpace(input.EntityID),
		Metadata:      sanitizedAuditMetadata(input.Metadata),
		CreatedAt:     now,
	}
	event.CustomerID = optionalAuditString(input.CustomerID)
	event.AccountID = optionalAuditString(input.AccountID)
	event.TransferID = optionalAuditString(input.TransferID)
	event.JournalEntryID = optionalAuditString(input.JournalEntryID)
	event.IdempotencyKey = optionalAuditString(input.IdempotencyKey)
	event.Reference = optionalAuditString(input.Reference)
	event.OldStatus = optionalAuditString(input.OldStatus)
	event.NewStatus = optionalAuditString(input.NewStatus)
	if event.InstitutionID == "" || event.Action == "" || event.EntityType == "" || event.EntityID == "" {
		return AuditEvent{}, "", ErrInvalidRequest
	}
	body, err := json.Marshal(event.Metadata)
	return event, string(body), err
}

func optionalAuditString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalAuditValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func formatAuditInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func sanitizedAuditMetadata(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" || unsafeAuditMetadataKey(key) {
			continue
		}
		out[key] = sanitizeAuditMetadataValue(value)
	}
	return out
}

func unsafeAuditMetadataKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "authorization") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "password") ||
		key == "bvn" ||
		key == "nin"
}

func sanitizeAuditMetadataValue(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if lower == "" {
		return ""
	}
	if strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "bvn") ||
		strings.Contains(lower, "nin") {
		return "[redacted]"
	}
	return value
}
