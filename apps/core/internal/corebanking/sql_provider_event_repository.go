package corebanking

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type sqlProviderEventRepository struct{}

func (r *sqlProviderEventRepository) insert(ctx context.Context, tx TxRunner, input RecordTransferInput, transferID string, now time.Time) error {
	var linkedTransferID *string
	if transferID != "" {
		linkedTransferID = &transferID
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO provider_events (id, institution_id, provider, provider_event_id, provider_reference, transfer_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.Must(uuid.NewRandom()).String(), input.InstitutionID, input.Provider, input.ProviderEventID, input.ProviderReference, linkedTransferID, now)
	return err
}

func (r *sqlProviderEventRepository) getTransfer(ctx context.Context, tx TxRunner, institutionID, provider, providerEventID string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+`
 WHERE institution_id = $1 AND id = (
	SELECT transfer_id FROM provider_events
	WHERE institution_id = $1 AND provider = $2 AND provider_event_id = $3 AND transfer_id IS NOT NULL
 )`, institutionID, provider, providerEventID)
	return &transfer, normalizeSQLError(err)
}

func (r *sqlProviderEventRepository) linkTransfer(ctx context.Context, tx TxRunner, transferID, institutionID, provider, providerEventID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE provider_events SET transfer_id = $1 WHERE institution_id = $2 AND provider = $3 AND provider_event_id = $4`, transferID, institutionID, provider, providerEventID)
	return err
}
