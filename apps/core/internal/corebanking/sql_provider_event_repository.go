package corebanking

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type sqlProviderEventRepository struct{}

func (r *sqlProviderEventRepository) reserve(ctx context.Context, tx TxRunner, input RecordTransferInput, transferID string, now time.Time) (bool, error) {
	var linkedTransferID *string
	if transferID != "" {
		linkedTransferID = &transferID
	}
	requestFingerprint := transferRequestFingerprint(input)
	result, err := tx.ExecContext(ctx, `
INSERT INTO provider_events (id, institution_id, provider, provider_event_id, provider_reference, transfer_id, request_fingerprint, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (institution_id, provider, provider_event_id) DO NOTHING`,
		uuid.Must(uuid.NewRandom()).String(), input.InstitutionID, input.Provider, input.ProviderEventID, input.ProviderReference, linkedTransferID, requestFingerprint, now)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (r *sqlProviderEventRepository) payloadMatches(ctx context.Context, tx TxRunner, input RecordTransferInput, requestFingerprint string) (bool, error) {
	var existingFingerprint string
	err := tx.GetContext(ctx, &existingFingerprint, `
SELECT request_fingerprint
FROM provider_events
WHERE institution_id = $1
  AND provider = $2
  AND provider_event_id = $3`, input.InstitutionID, input.Provider, input.ProviderEventID)
	if err != nil {
		return false, normalizeSQLError(err)
	}
	if existingFingerprint == "" {
		return true, nil
	}
	return existingFingerprint == requestFingerprint, nil
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
	result, err := tx.ExecContext(ctx, `
UPDATE provider_events
SET transfer_id = $1
WHERE institution_id = $2
  AND provider = $3
  AND provider_event_id = $4
  AND (transfer_id IS NULL OR transfer_id = $1)`, transferID, institutionID, provider, providerEventID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}
