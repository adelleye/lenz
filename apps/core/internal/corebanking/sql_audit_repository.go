package corebanking

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

type sqlAuditRepository struct {
	db *sqlx.DB
}

type auditEventRow struct {
	AuditEvent
	MetadataJSON []byte `db:"metadata"`
}

func (r *sqlAuditRepository) ListAuditEvents(ctx context.Context, institutionID string) ([]AuditEvent, error) {
	rows := []auditEventRow{}
	if err := r.db.SelectContext(ctx, &rows, `
SELECT id, institution_id, actor_type, actor_id, request_id, action, entity_type, entity_id,
       customer_id, account_id, transfer_id, journal_entry_id, idempotency_key, reference,
       old_status, new_status, metadata, created_at
FROM audit_events
WHERE institution_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 200`, institutionID); err != nil {
		return nil, err
	}
	events := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		event := row.AuditEvent
		if len(row.MetadataJSON) > 0 {
			if err := json.Unmarshal(row.MetadataJSON, &event.Metadata); err != nil {
				return nil, err
			}
		}
		if event.Metadata == nil {
			event.Metadata = map[string]string{}
		}
		events = append(events, event)
	}
	return events, nil
}

func insertAuditEvent(ctx context.Context, tx TxRunner, input auditEventInput) (*AuditEvent, error) {
	event, metadata, err := newAuditEvent(ctx, input)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO audit_events (
    id, institution_id, actor, actor_type, actor_id, request_id, action,
    subject_type, subject_id, entity_type, entity_id, customer_id, account_id,
    transfer_id, journal_entry_id, idempotency_key, reference, old_status,
    new_status, meta, metadata, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7,
        $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18,
        $19, $20::jsonb, $20::jsonb, $21)`,
		event.ID,
		event.InstitutionID,
		event.ActorType,
		event.ActorType,
		event.ActorID,
		event.RequestID,
		event.Action,
		event.EntityType,
		event.EntityID,
		event.EntityType,
		event.EntityID,
		event.CustomerID,
		event.AccountID,
		event.TransferID,
		event.JournalEntryID,
		event.IdempotencyKey,
		event.Reference,
		event.OldStatus,
		event.NewStatus,
		metadata,
		event.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &event, nil
}
