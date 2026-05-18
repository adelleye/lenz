package corebanking

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type sqlTransferRepository struct {
	db             *sqlx.DB
	ledger         *sqlLedgerRepository
	holds          *sqlHoldRepository
	providerEvents *sqlProviderEventRepository
}

const transferSelectSQL = `SELECT id, institution_id, account_id, direction, status, provider_status, ledger_status, reconciliation_status, amount_minor, currency_id, idempotency_key, provider, provider_reference, provider_event_id, journal_entry_id, reversal_of_transfer_id, failure_reason, narration, created_at, updated_at FROM transfers`

func (r *sqlTransferRepository) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	var transfer Transfer
	err := r.db.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND id = $2`, institutionID, transferID)
	return &transfer, normalizeSQLError(err)
}

func (r *sqlTransferRepository) GetTransferByIdempotency(ctx context.Context, institutionID, idempotencyKey string) (*Transfer, error) {
	var transfer Transfer
	err := r.db.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND idempotency_key = $2`, institutionID, strings.TrimSpace(idempotencyKey))
	return &transfer, normalizeSQLError(err)
}

func (r *sqlTransferRepository) ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error) {
	var transfers []Transfer
	err := r.db.SelectContext(ctx, &transfers, transferSelectSQL+` WHERE institution_id = $1 ORDER BY created_at DESC LIMIT 100`, institutionID)
	return transfers, err
}

func (r *sqlTransferRepository) RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error) {
	var transfer *Transfer
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		var err error
		transfer, err = r.recordTransfer(ctx, tx, input)
		return err
	})
	return transfer, err
}

func (r *sqlTransferRepository) ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error) {
	var transfer *Transfer
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		input.InstitutionID = strings.TrimSpace(input.InstitutionID)
		input.TransferID = strings.TrimSpace(input.TransferID)
		input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
		if input.InstitutionID == "" || input.TransferID == "" || input.IdempotencyKey == "" {
			return ErrInvalidRequest
		}

		original, err := r.getTransferForUpdate(ctx, tx, input.InstitutionID, input.TransferID)
		if err != nil {
			return err
		}
		if original.Status != TransferStatusSucceeded || original.JournalEntryID == nil || original.Direction == TransferDirectionReversal {
			return ErrInvalidRequest
		}

		provider := strings.TrimSpace(input.Provider)
		if provider == "" {
			provider = original.Provider
		}
		providerReference := strings.TrimSpace(input.ProviderReference)
		if providerReference == "" {
			originalReference := strings.TrimSpace(original.ProviderReference)
			if originalReference == "" {
				originalReference = original.ID
			}
			providerReference = "reversal:" + originalReference
		}
		narration := strings.TrimSpace(input.Narration)
		if narration == "" {
			narration = "Reversal of " + original.ID
		}

		direction := TransferDirectionOutbound
		if original.Direction == TransferDirectionOutbound {
			direction = TransferDirectionInbound
		}
		transfer, err = r.recordTransfer(ctx, tx, RecordTransferInput{
			InstitutionID:        input.InstitutionID,
			AccountID:            original.AccountID,
			ClearingAccountID:    DemoClearingAccountID,
			Direction:            direction,
			Status:               TransferStatusSucceeded,
			AmountMinor:          original.AmountMinor,
			CurrencyID:           original.CurrencyID,
			IdempotencyKey:       input.IdempotencyKey,
			Provider:             provider,
			ProviderReference:    providerReference,
			ProviderEventID:      strings.TrimSpace(input.ProviderEventID),
			ReversalOfTransferID: original.ID,
			FailureReason:        strings.TrimSpace(input.FailureReason),
			Narration:            narration,
		})
		if err != nil {
			return err
		}
		if transfer.ReversalOfTransferID == nil || *transfer.ReversalOfTransferID != original.ID {
			return ErrConflict
		}
		transfer.Direction = TransferDirectionReversal
		if _, err = tx.ExecContext(ctx, `UPDATE transfers SET direction = 'reversal', updated_at = $1 WHERE institution_id = $2 AND id = $3`, time.Now().UTC(), input.InstitutionID, transfer.ID); err != nil {
			return err
		}
		return nil
	})
	return transfer, err
}

func (r *sqlTransferRepository) recordTransfer(ctx context.Context, tx TxRunner, input RecordTransferInput) (*Transfer, error) {
	if input.IdempotencyKey != "" {
		if err := lockReplayKey(ctx, tx, "idempotency", input.InstitutionID, input.IdempotencyKey); err != nil {
			return nil, err
		}
	}
	if input.ProviderEventID != "" {
		if err := lockReplayKey(ctx, tx, "provider_event", input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, err
		}
	}
	if input.ProviderReference != "" && input.Status != TransferStatusPending {
		if err := lockReplayKey(ctx, tx, "provider_reference", input.InstitutionID, input.Provider, input.ProviderReference, input.Direction); err != nil {
			return nil, err
		}
	}

	if existing, err := r.getTransferByIdempotency(ctx, tx, input.InstitutionID, input.IdempotencyKey); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if input.ProviderEventID != "" {
		if existing, err := r.providerEvents.getTransfer(ctx, tx, input.InstitutionID, input.Provider, input.ProviderEventID); err == nil {
			return existing, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	if input.ProviderReference != "" && input.Status != TransferStatusPending {
		if pending, err := r.getPendingTransferByProviderReference(ctx, tx, input.InstitutionID, input.Provider, input.ProviderReference, input.Direction); err == nil {
			return r.settlePendingTransfer(ctx, tx, *pending, input)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if settled, err := r.getSettledTransferByProviderReference(ctx, tx, input.InstitutionID, input.Provider, input.ProviderReference, input.Direction); err == nil {
			if !sameTransferReplay(settled, input) {
				return nil, ErrConflict
			}
			return settled, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	account, err := lockAccountBalance(ctx, tx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if _, err = lockAccountBalance(ctx, tx, input.InstitutionID, input.ClearingAccountID); err != nil {
		return nil, err
	}

	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := input.Status
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	failureReason := input.FailureReason
	if customerInitiatedOutbound(input) && !canUseAvailableBalance(account.Account, account.Balance.AvailableMinor, input.AmountMinor) {
		status = TransferStatusFailed
		failureReason = "insufficient_funds"
	}
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	if status == TransferStatusSucceeded && wouldCreateReversalDeficit(account.Account, account.Balance, input) {
		ledgerStatus = LedgerStatusReversalDeficit
		reconciliationStatus = ReconciliationStatusManualReview
	}

	transferID := uuid.Must(uuid.NewRandom()).String()
	now := time.Now().UTC()
	var providerEventID *string
	if input.ProviderEventID != "" {
		providerEventID = &input.ProviderEventID
	}
	var reversalOf *string
	if input.ReversalOfTransferID != "" {
		reversalOf = &input.ReversalOfTransferID
	}
	var failure *string
	if failureReason != "" {
		failure = &failureReason
	}

	if input.ProviderEventID != "" {
		inserted, err := r.providerEvents.reserve(ctx, tx, input, "", now)
		if err != nil {
			return nil, err
		}
		if !inserted {
			return existingProviderEventTransfer(ctx, tx, r.providerEvents, input)
		}
	}

	var journalEntryID *string
	if status == TransferStatusSucceeded {
		journalID, err := r.ledger.postJournal(ctx, tx, input, transferID, now, postingBalanceOptions{})
		if err != nil {
			return nil, err
		}
		journalEntryID = &journalID
	}

	transfer := Transfer{
		ID:                   transferID,
		InstitutionID:        input.InstitutionID,
		AccountID:            input.AccountID,
		Direction:            input.Direction,
		Status:               status,
		ProviderStatus:       providerStatus,
		LedgerStatus:         ledgerStatus,
		ReconciliationStatus: reconciliationStatus,
		AmountMinor:          input.AmountMinor,
		CurrencyID:           input.CurrencyID,
		IdempotencyKey:       input.IdempotencyKey,
		Provider:             input.Provider,
		ProviderReference:    input.ProviderReference,
		ProviderEventID:      providerEventID,
		JournalEntryID:       journalEntryID,
		ReversalOfTransferID: reversalOf,
		FailureReason:        failure,
		Narration:            input.Narration,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if _, err = tx.NamedExecContext(ctx, `
INSERT INTO transfers (id, institution_id, account_id, direction, status, provider_status, ledger_status, reconciliation_status, amount_minor, currency_id, idempotency_key, provider, provider_reference, provider_event_id, journal_entry_id, reversal_of_transfer_id, failure_reason, narration, created_at, updated_at)
VALUES (:id, :institution_id, :account_id, :direction, :status, :provider_status, :ledger_status, :reconciliation_status, :amount_minor, :currency_id, :idempotency_key, :provider, :provider_reference, :provider_event_id, :journal_entry_id, :reversal_of_transfer_id, :failure_reason, :narration, :created_at, :updated_at)`, transfer); err != nil {
		return nil, err
	}
	if status == TransferStatusPending && input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == "" {
		if err = r.holds.create(ctx, tx, transfer, now); err != nil {
			return nil, err
		}
	}
	if input.ProviderEventID != "" {
		if err = r.providerEvents.linkTransfer(ctx, tx, transfer.ID, input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, err
		}
	}
	return &transfer, nil
}

func (r *sqlTransferRepository) settlePendingTransfer(ctx context.Context, tx TxRunner, pending Transfer, input RecordTransferInput) (*Transfer, error) {
	if pending.Direction != input.Direction || pending.AccountID != input.AccountID || pending.AmountMinor != input.AmountMinor || pending.CurrencyID != input.CurrencyID {
		return nil, ErrConflict
	}
	account, err := lockAccountBalance(ctx, tx, pending.InstitutionID, pending.AccountID)
	if err != nil {
		return nil, err
	}
	if _, err = lockAccountBalance(ctx, tx, pending.InstitutionID, input.ClearingAccountID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if input.ProviderEventID != "" {
		inserted, err := r.providerEvents.reserve(ctx, tx, input, pending.ID, now)
		if err != nil {
			return nil, err
		}
		if !inserted {
			return existingProviderEventTransfer(ctx, tx, r.providerEvents, input)
		}
	}

	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := input.Status
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	failureReason := input.FailureReason
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	var journalEntryID *string

	switch status {
	case TransferStatusSucceeded:
		if customerInitiatedOutbound(input) && !account.AllowNegative {
			if _, err = r.holds.getActiveForTransfer(ctx, tx, pending.InstitutionID, pending.ID); err != nil {
				return nil, err
			}
		}
		if wouldCreateReversalDeficit(account.Account, account.Balance, input) {
			ledgerStatus = LedgerStatusReversalDeficit
			reconciliationStatus = ReconciliationStatusManualReview
		}
		options := postingBalanceOptions{}
		if pending.Direction == TransferDirectionOutbound && pending.ReversalOfTransferID == nil {
			options.HeldAccountID = pending.AccountID
		}
		journalID, err := r.ledger.postJournal(ctx, tx, input, pending.ID, now, options)
		if err != nil {
			return nil, err
		}
		journalEntryID = &journalID
		if options.HeldAccountID != "" {
			if err = r.holds.consume(ctx, tx, pending.InstitutionID, pending.ID, now); err != nil {
				return nil, err
			}
		}
	case TransferStatusFailed:
		if pending.Direction == TransferDirectionOutbound && pending.ReversalOfTransferID == nil {
			if err = r.holds.release(ctx, tx, pending.InstitutionID, pending.ID, now); err != nil {
				return nil, err
			}
		}
	default:
		return nil, ErrInvalidRequest
	}

	var providerEventID *string
	if input.ProviderEventID != "" {
		providerEventID = &input.ProviderEventID
	} else {
		providerEventID = pending.ProviderEventID
	}
	var failure *string
	if failureReason != "" {
		failure = &failureReason
	}
	narration := strings.TrimSpace(input.Narration)
	if narration == "" {
		narration = pending.Narration
	}

	if _, err = tx.ExecContext(ctx, `
UPDATE transfers
SET status = $1,
    provider_status = $2,
    ledger_status = $3,
    reconciliation_status = $4,
    provider_event_id = $5,
    journal_entry_id = $6,
    failure_reason = $7,
    narration = $8,
    updated_at = $9
WHERE institution_id = $10 AND id = $11`,
		status, providerStatus, ledgerStatus, reconciliationStatus, providerEventID, journalEntryID, failure, narration, now, pending.InstitutionID, pending.ID); err != nil {
		return nil, err
	}
	return r.getTransferForUpdate(ctx, tx, pending.InstitutionID, pending.ID)
}

func (r *sqlTransferRepository) getTransferForUpdate(ctx context.Context, tx TxRunner, institutionID, transferID string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND id = $2 FOR UPDATE`, institutionID, transferID)
	return &transfer, normalizeSQLError(err)
}

func (r *sqlTransferRepository) getTransferByIdempotency(ctx context.Context, tx TxRunner, institutionID, idempotencyKey string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND idempotency_key = $2 FOR UPDATE`, institutionID, idempotencyKey)
	return &transfer, normalizeSQLError(err)
}

func (r *sqlTransferRepository) getPendingTransferByProviderReference(ctx context.Context, tx TxRunner, institutionID, provider, providerReference, direction string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+`
 WHERE institution_id = $1
   AND provider = $2
   AND provider_reference = $3
   AND direction = $4
   AND status = 'pending'
 ORDER BY created_at
 LIMIT 1
 FOR UPDATE`, institutionID, provider, providerReference, direction)
	return &transfer, normalizeSQLError(err)
}

func (r *sqlTransferRepository) getSettledTransferByProviderReference(ctx context.Context, tx TxRunner, institutionID, provider, providerReference, direction string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+`
 WHERE institution_id = $1
   AND provider = $2
   AND provider_reference = $3
   AND direction = $4
   AND status <> 'pending'
 ORDER BY updated_at DESC, created_at DESC
 LIMIT 1
 FOR UPDATE`, institutionID, provider, providerReference, direction)
	return &transfer, normalizeSQLError(err)
}

func lockReplayKey(ctx context.Context, tx TxRunner, scope string, parts ...string) error {
	keyParts := make([]string, 0, len(parts)+2)
	keyParts = append(keyParts, "lenz-core", "corebanking", strings.TrimSpace(scope))
	for _, part := range parts {
		keyParts = append(keyParts, strings.TrimSpace(part))
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, strings.Join(keyParts, "\x1f"))
	return err
}

func existingProviderEventTransfer(ctx context.Context, tx TxRunner, providerEvents *sqlProviderEventRepository, input RecordTransferInput) (*Transfer, error) {
	existing, err := providerEvents.getTransfer(ctx, tx, input.InstitutionID, input.Provider, input.ProviderEventID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrConflict
	}
	return existing, err
}

func sameTransferReplay(transfer *Transfer, input RecordTransferInput) bool {
	if transfer == nil {
		return false
	}
	return transfer.InstitutionID == input.InstitutionID &&
		transfer.Provider == input.Provider &&
		transfer.ProviderReference == input.ProviderReference &&
		transfer.Direction == input.Direction &&
		transfer.AccountID == input.AccountID &&
		transfer.AmountMinor == input.AmountMinor &&
		transfer.CurrencyID == input.CurrencyID
}
