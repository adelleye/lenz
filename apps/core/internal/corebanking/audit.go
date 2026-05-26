package corebanking

import (
	"context"
	"encoding/json"
	"lenz-core/apps/auth/authn"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const (
	AuditActionCustomerCreated           = "customer.created"
	AuditActionAccountCreated            = "account.created"
	AuditActionInternalCreditPosted      = "internal_credit.posted"
	AuditActionInternalDebitPosted       = "internal_debit.posted"
	AuditActionInternalTransferPosted    = "internal_transfer.posted"
	AuditActionAccountFrozen             = "account.frozen"
	AuditActionAccountUnfrozen           = "account.unfrozen"
	AuditActionPNDActivated              = "account.pnd_activated"
	AuditActionPNDDeactivated            = "account.pnd_deactivated"
	AuditActionLienPlaced                = "account.lien_placed"
	AuditActionLienReleased              = "account.lien_released"
	AuditActionReconciliationReviewed    = "reconciliation.reviewed"
	AuditActionExternalOutboundSucceeded = "external_outbound.succeeded"
	AuditActionExternalOutboundFailed    = "external_outbound.failed"
	AuditActionExternalOutboundPending   = "external_outbound.pending"
	AuditActionExternalOutboundUnknown   = "external_outbound.provider_unknown"
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

func newAuditEvent(ctx context.Context, input auditEventInput) (AuditEvent, string, error) {
	now := input.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	actorType, actorID, requestID, contextMetadata := auditContext(ctx)
	event := AuditEvent{
		ID:            uuid.Must(uuid.NewRandom()).String(),
		InstitutionID: strings.TrimSpace(input.InstitutionID),
		ActorType:     actorType,
		ActorID:       actorID,
		RequestID:     requestID,
		Action:        strings.TrimSpace(input.Action),
		EntityType:    strings.TrimSpace(input.EntityType),
		EntityID:      strings.TrimSpace(input.EntityID),
		Metadata:      mergeAuditMetadata(sanitizedAuditMetadata(input.Metadata), contextMetadata),
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

func auditContext(ctx context.Context) (string, string, string, map[string]string) {
	actorType, actorID, requestID := "system", "system", "service"
	metadata := map[string]string{}
	if reqID := strings.TrimSpace(middleware.GetReqID(ctx)); reqID != "" {
		requestID = reqID
	}
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorType = strings.TrimSpace(principal.ActorType)
		if actorType == "" {
			actorType = "principal"
		}
		actorID = strings.TrimSpace(principal.ActorID)
		if actorID == "" {
			actorID = "unknown"
		}
		if len(principal.Roles) > 0 {
			metadata["actor_roles"] = strings.Join(principal.Roles, ",")
		}
		if len(principal.Scopes) > 0 {
			metadata["actor_scopes"] = strings.Join(principal.Scopes, ",")
		}
		if sourceIP := strings.TrimSpace(principal.SourceIP); sourceIP != "" {
			metadata["source_ip"] = sourceIP
		}
		if userAgent := strings.TrimSpace(principal.UserAgent); userAgent != "" {
			metadata["user_agent"] = userAgent
		}
	}
	return actorType, actorID, requestID, metadata
}

func mergeAuditMetadata(base, extra map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for key, value := range sanitizedAuditMetadata(extra) {
		base[key] = value
	}
	return base
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
		strings.Contains(key, "credential") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
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
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "api key") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "bvn") ||
		strings.Contains(lower, "nin") {
		return "[redacted]"
	}
	return value
}
