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

const transferSelectSQL = `SELECT id, institution_id, account_id, direction, status, provider_status, ledger_status, reconciliation_status, amount_minor, currency_id, idempotency_key, provider, provider_reference, provider_event_id, journal_entry_id, reversal_of_transfer_id, request_fingerprint, failure_reason, narration, created_at, updated_at FROM transfers`

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
		transfer, _, err = r.recordTransfer(ctx, tx, input)
		return err
	})
	return transfer, err
}

func (r *sqlTransferRepository) BeginExternalOutboundTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, bool, error) {
	var transfer *Transfer
	created := false
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		var err error
		transfer, created, err = r.recordTransfer(ctx, tx, input)
		return err
	})
	return transfer, created, err
}

func (r *sqlTransferRepository) CompleteExternalOutboundTransfer(ctx context.Context, transferID string, input RecordTransferInput) (*Transfer, error) {
	var transfer *Transfer
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		var err error
		transfer, err = r.completeExternalOutboundTransfer(ctx, tx, strings.TrimSpace(transferID), input)
		return err
	})
	return transfer, err
}

func (r *sqlTransferRepository) GetTransferHold(ctx context.Context, institutionID, transferID string) (*AccountHold, error) {
	return r.holds.getForTransfer(ctx, r.db, institutionID, transferID)
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
		counterpartyAccountID, err := r.originalCounterpartyAccountID(ctx, tx, *original)
		if err != nil {
			return err
		}
		transfer, _, err = r.recordTransfer(ctx, tx, RecordTransferInput{
			InstitutionID:        input.InstitutionID,
			AccountID:            original.AccountID,
			ClearingAccountID:    counterpartyAccountID,
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

func (r *sqlTransferRepository) originalCounterpartyAccountID(ctx context.Context, tx TxRunner, original Transfer) (string, error) {
	if original.JournalEntryID == nil {
		return "", ErrInvalidRequest
	}
	var accountIDs []string
	if err := tx.SelectContext(ctx, &accountIDs, `
SELECT account_id
FROM postings
WHERE institution_id = $1
  AND journal_entry_id = $2
  AND account_id <> $3
ORDER BY account_id`, original.InstitutionID, *original.JournalEntryID, original.AccountID); err != nil {
		return "", err
	}
	if len(accountIDs) != 1 {
		return "", ErrDataIntegrity
	}
	return accountIDs[0], nil
}

func (r *sqlTransferRepository) recordTransfer(ctx context.Context, tx TxRunner, input RecordTransferInput) (*Transfer, bool, error) {
	requestFingerprint := transferRequestFingerprint(input)
	if input.IdempotencyKey != "" {
		if err := lockReplayKey(ctx, tx, "idempotency", input.InstitutionID, input.IdempotencyKey); err != nil {
			return nil, false, err
		}
	}
	if input.ProviderEventID != "" {
		if err := lockReplayKey(ctx, tx, "provider_event", input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, false, err
		}
	}
	if input.ProviderReference != "" && input.Status != TransferStatusPending {
		if err := lockReplayKey(ctx, tx, "provider_reference", input.InstitutionID, input.Provider, input.ProviderReference, input.Direction); err != nil {
			return nil, false, err
		}
	}

	if existing, err := r.getTransferByIdempotency(ctx, tx, input.InstitutionID, input.IdempotencyKey); err == nil {
		ok, err := r.sameTransferReplay(ctx, tx, existing, input, requestFingerprint)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, ErrConflict
		}
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	if input.ProviderEventID != "" {
		if existing, err := r.providerEvents.getTransfer(ctx, tx, input.InstitutionID, input.Provider, input.ProviderEventID); err == nil {
			ok, err := r.providerEvents.payloadMatches(ctx, tx, input, requestFingerprint)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				return nil, false, ErrConflict
			}
			return existing, false, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
	}

	if input.ProviderReference != "" && input.Status != TransferStatusPending {
		if pending, err := r.getPendingTransferByProviderReference(ctx, tx, input.InstitutionID, input.Provider, input.ProviderReference, input.Direction); err == nil {
			transfer, err := r.settlePendingTransfer(ctx, tx, *pending, input)
			return transfer, false, err
		} else if !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
		if settled, err := r.getSettledTransferByProviderReference(ctx, tx, input.InstitutionID, input.Provider, input.ProviderReference, input.Direction); err == nil {
			ok, err := r.sameTransferReplayByFields(ctx, tx, settled, input)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				return nil, false, ErrConflict
			}
			return settled, false, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
	}

	account, clearing, err := lockTransferAccountBalances(ctx, tx, input.InstitutionID, input.AccountID, input.ClearingAccountID)
	if err != nil {
		return nil, false, err
	}
	if err := enforceTransferControls(input, account.Account, clearing.Account); err != nil {
		return nil, false, err
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
	insufficient := customerInitiatedOutbound(input) && !canUseAvailableBalance(account.Account, account.Balance.AvailableMinor, input.AmountMinor)
	if customerInitiatedOutbound(input) && input.RequireAvailable && account.Balance.AvailableMinor < input.AmountMinor {
		insufficient = true
	}
	if insufficient {
		if input.RejectInsufficient {
			return nil, false, ErrInsufficient
		}
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
			return nil, false, err
		}
		if !inserted {
			transfer, err := existingProviderEventTransfer(ctx, tx, r.providerEvents, input, requestFingerprint)
			return transfer, false, err
		}
	}

	var journalEntryID *string
	if status == TransferStatusSucceeded {
		journalID, err := r.ledger.postJournal(ctx, tx, input, transferID, now, postingBalanceOptions{})
		if err != nil {
			return nil, false, err
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
		RequestFingerprint:   requestFingerprint,
		FailureReason:        failure,
		Narration:            input.Narration,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if _, err = tx.NamedExecContext(ctx, `
INSERT INTO transfers (id, institution_id, account_id, direction, status, provider_status, ledger_status, reconciliation_status, amount_minor, currency_id, idempotency_key, provider, provider_reference, provider_event_id, journal_entry_id, reversal_of_transfer_id, request_fingerprint, failure_reason, narration, created_at, updated_at)
VALUES (:id, :institution_id, :account_id, :direction, :status, :provider_status, :ledger_status, :reconciliation_status, :amount_minor, :currency_id, :idempotency_key, :provider, :provider_reference, :provider_event_id, :journal_entry_id, :reversal_of_transfer_id, :request_fingerprint, :failure_reason, :narration, :created_at, :updated_at)`, transfer); err != nil {
		return nil, false, err
	}
	if status == TransferStatusPending && input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == "" {
		if err = r.holds.create(ctx, tx, transfer, now); err != nil {
			return nil, false, err
		}
	}
	if input.ProviderEventID != "" {
		if err = r.providerEvents.linkTransfer(ctx, tx, transfer.ID, input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, false, err
		}
	}
	if err = auditPostedInternalTransfer(ctx, tx, input, transfer, account.Account, clearing.Account); err != nil {
		return nil, false, err
	}
	return &transfer, true, nil
}

func (r *sqlTransferRepository) settlePendingTransfer(ctx context.Context, tx TxRunner, pending Transfer, input RecordTransferInput) (*Transfer, error) {
	if pending.Direction != input.Direction || pending.AccountID != input.AccountID || pending.AmountMinor != input.AmountMinor || pending.CurrencyID != input.CurrencyID {
		return nil, ErrConflict
	}
	account, clearing, err := lockTransferAccountBalances(ctx, tx, pending.InstitutionID, pending.AccountID, input.ClearingAccountID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if input.ProviderEventID != "" {
		inserted, err := r.providerEvents.reserve(ctx, tx, input, pending.ID, now)
		if err != nil {
			return nil, err
		}
		if !inserted {
			return existingProviderEventTransfer(ctx, tx, r.providerEvents, input, transferRequestFingerprint(input))
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
		if err := enforceTransferControls(input, account.Account, clearing.Account); err != nil {
			return nil, err
		}
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
	updated, err := r.getTransferForUpdate(ctx, tx, pending.InstitutionID, pending.ID)
	if err != nil {
		return nil, err
	}
	if err = auditPostedInternalTransfer(ctx, tx, input, *updated, account.Account, clearing.Account); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *sqlTransferRepository) completeExternalOutboundTransfer(ctx context.Context, tx TxRunner, transferID string, input RecordTransferInput) (*Transfer, error) {
	if transferID == "" || input.InstitutionID == "" || input.AccountID == "" || input.ClearingAccountID == "" {
		return nil, ErrInvalidRequest
	}
	if input.ProviderEventID != "" {
		if err := lockReplayKey(ctx, tx, "provider_event", input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, err
		}
	}

	pending, err := r.getTransferForUpdate(ctx, tx, input.InstitutionID, transferID)
	if err != nil {
		return nil, err
	}
	if pending.Direction != TransferDirectionOutbound ||
		pending.AccountID != input.AccountID ||
		pending.AmountMinor != input.AmountMinor ||
		pending.CurrencyID != input.CurrencyID ||
		pending.Provider != input.Provider {
		return nil, ErrConflict
	}
	if input.RequestFingerprint != "" && pending.RequestFingerprint != "" && input.RequestFingerprint != pending.RequestFingerprint {
		return nil, ErrConflict
	}
	if pending.Status != TransferStatusPending {
		return pending, nil
	}

	account, clearing, err := lockTransferAccountBalances(ctx, tx, pending.InstitutionID, pending.AccountID, input.ClearingAccountID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if input.ProviderEventID != "" {
		inserted, err := r.providerEvents.reserve(ctx, tx, input, pending.ID, now)
		if err != nil {
			return nil, err
		}
		if !inserted {
			return existingProviderEventTransfer(ctx, tx, r.providerEvents, input, transferRequestFingerprint(input))
		}
	}

	status, providerStatus, err := externalOutboundTransferStatuses(input)
	if err != nil {
		return nil, err
	}

	failureReason := strings.TrimSpace(input.FailureReason)
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	var journalEntryID *string

	switch status {
	case TransferStatusSucceeded:
		if err := enforceTransferControls(input, account.Account, clearing.Account); err != nil {
			return nil, err
		}
		if customerInitiatedOutbound(input) && !account.AllowNegative {
			if _, err = r.holds.getActiveForTransfer(ctx, tx, pending.InstitutionID, pending.ID); err != nil {
				return nil, err
			}
		}
		if wouldCreateReversalDeficit(account.Account, account.Balance, input) {
			ledgerStatus = LedgerStatusReversalDeficit
			reconciliationStatus = ReconciliationStatusManualReview
		}
		journalID, err := r.ledger.postJournal(ctx, tx, input, pending.ID, now, postingBalanceOptions{HeldAccountID: pending.AccountID})
		if err != nil {
			return nil, err
		}
		journalEntryID = &journalID
		if err = r.holds.consume(ctx, tx, pending.InstitutionID, pending.ID, now); err != nil {
			return nil, err
		}
	case TransferStatusFailed:
		if err = r.holds.release(ctx, tx, pending.InstitutionID, pending.ID, now); err != nil {
			return nil, err
		}
	case TransferStatusPending:
		if _, err = r.holds.getActiveForTransfer(ctx, tx, pending.InstitutionID, pending.ID); err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidRequest
	}

	providerReference := strings.TrimSpace(input.ProviderReference)
	if providerReference == "" {
		providerReference = pending.ProviderReference
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
    provider_reference = $5,
    provider_event_id = $6,
    journal_entry_id = $7,
    failure_reason = $8,
    narration = $9,
    updated_at = $10
WHERE institution_id = $11 AND id = $12`,
		status, providerStatus, ledgerStatus, reconciliationStatus, providerReference, providerEventID, journalEntryID, failure, narration, now, pending.InstitutionID, pending.ID); err != nil {
		return nil, err
	}
	updated, err := r.getTransferForUpdate(ctx, tx, pending.InstitutionID, pending.ID)
	if err != nil {
		return nil, err
	}
	if input.ProviderEventID != "" {
		if err = r.providerEvents.linkTransfer(ctx, tx, updated.ID, input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, err
		}
	}
	if err = auditExternalOutboundTransfer(ctx, tx, input, *updated, account.Account, clearing.Account); err != nil {
		return nil, err
	}
	return updated, nil
}

func externalOutboundTransferStatuses(input RecordTransferInput) (string, string, error) {
	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	if status == "" {
		status = providerStatus
	}
	if !validTransferStatus(status) || !validProviderStatus(providerStatus) {
		return "", "", ErrInvalidRequest
	}
	return status, providerStatus, nil
}

func auditPostedInternalTransfer(ctx context.Context, tx TxRunner, input RecordTransferInput, transfer Transfer, account, clearing Account) error {
	auditInput, ok := postedInternalTransferAuditInput(input, transfer, account, clearing)
	if !ok {
		return nil
	}
	_, err := insertAuditEvent(ctx, tx, auditInput)
	return err
}

func auditExternalOutboundTransfer(ctx context.Context, tx TxRunner, input RecordTransferInput, transfer Transfer, account, clearing Account) error {
	auditInput, ok := externalOutboundTransferAuditInput(input, transfer, account, clearing)
	if !ok {
		return nil
	}
	_, err := insertAuditEvent(ctx, tx, auditInput)
	return err
}

func postedInternalTransferAuditInput(input RecordTransferInput, transfer Transfer, account, clearing Account) (auditEventInput, bool) {
	if transfer.Status != TransferStatusSucceeded || input.Provider != ProviderLedgerInternal || input.ReversalOfTransferID != "" {
		return auditEventInput{}, false
	}
	action := AuditActionInternalCreditPosted
	metadata := map[string]string{
		"amount_minor": formatAuditInt(input.AmountMinor),
		"currency_id":  input.CurrencyID,
	}
	accountID := account.ID
	if input.Direction == TransferDirectionOutbound {
		action = AuditActionInternalDebitPosted
		if clearing.Kind == AccountKindCustomer {
			action = AuditActionInternalTransferPosted
			metadata["source_account_id"] = account.ID
			metadata["destination_account_id"] = clearing.ID
		}
	}
	return auditEventInput{
		InstitutionID:  transfer.InstitutionID,
		Action:         action,
		EntityType:     "transfer",
		EntityID:       transfer.ID,
		AccountID:      accountID,
		TransferID:     transfer.ID,
		JournalEntryID: optionalAuditValue(transfer.JournalEntryID),
		IdempotencyKey: input.IdempotencyKey,
		Reference:      input.ProviderReference,
		Metadata:       metadata,
		CreatedAt:      transfer.CreatedAt,
	}, true
}

func externalOutboundTransferAuditInput(input RecordTransferInput, transfer Transfer, account, clearing Account) (auditEventInput, bool) {
	if transfer.Direction != TransferDirectionOutbound || input.Provider == ProviderLedgerInternal || input.ReversalOfTransferID != "" {
		return auditEventInput{}, false
	}
	action := AuditActionExternalOutboundPending
	switch {
	case transfer.ProviderStatus == TransferProviderStatusUnknown:
		action = AuditActionExternalOutboundUnknown
	case transfer.Status == TransferStatusSucceeded:
		action = AuditActionExternalOutboundSucceeded
	case transfer.Status == TransferStatusFailed:
		action = AuditActionExternalOutboundFailed
	}
	metadata := map[string]string{
		"amount_minor":          formatAuditInt(transfer.AmountMinor),
		"currency_id":           transfer.CurrencyID,
		"provider":              transfer.Provider,
		"provider_status":       transfer.ProviderStatus,
		"ledger_status":         transfer.LedgerStatus,
		"reconciliation_status": transfer.ReconciliationStatus,
		"clearing_account_id":   clearing.ID,
		"account_status":        account.Status,
	}
	if transfer.FailureReason != nil {
		metadata["failure_reason"] = *transfer.FailureReason
	}
	return auditEventInput{
		InstitutionID:  transfer.InstitutionID,
		Action:         action,
		EntityType:     "transfer",
		EntityID:       transfer.ID,
		AccountID:      account.ID,
		TransferID:     transfer.ID,
		JournalEntryID: optionalAuditValue(transfer.JournalEntryID),
		IdempotencyKey: transfer.IdempotencyKey,
		Reference:      transfer.ProviderReference,
		NewStatus:      transfer.Status,
		Metadata:       metadata,
		CreatedAt:      transfer.UpdatedAt,
	}, true
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

func existingProviderEventTransfer(ctx context.Context, tx TxRunner, providerEvents *sqlProviderEventRepository, input RecordTransferInput, requestFingerprint string) (*Transfer, error) {
	ok, err := providerEvents.payloadMatches(ctx, tx, input, requestFingerprint)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrConflict
	}
	existing, err := providerEvents.getTransfer(ctx, tx, input.InstitutionID, input.Provider, input.ProviderEventID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrConflict
	}
	return existing, err
}

func (r *sqlTransferRepository) sameTransferReplay(ctx context.Context, tx TxRunner, transfer *Transfer, input RecordTransferInput, requestFingerprint string) (bool, error) {
	if transferRequestFingerprintMatches(transfer, requestFingerprint) {
		return true, nil
	}
	if strings.TrimSpace(transfer.RequestFingerprint) != "" {
		return false, nil
	}
	return r.sameTransferReplayByFields(ctx, tx, transfer, input)
}

func (r *sqlTransferRepository) sameTransferReplayByFields(ctx context.Context, tx TxRunner, transfer *Transfer, input RecordTransferInput) (bool, error) {
	if !sameTransferReplayFields(transfer, input) {
		return false, nil
	}
	if transfer.JournalEntryID == nil || input.ClearingAccountID == "" {
		return false, nil
	}
	counterpartyAccountID, err := r.originalCounterpartyAccountID(ctx, tx, *transfer)
	if err != nil {
		return false, err
	}
	return counterpartyAccountID == input.ClearingAccountID, nil
}

func sameTransferReplayFields(transfer *Transfer, input RecordTransferInput) bool {
	if transfer == nil {
		return false
	}
	reversalOfTransferID := ""
	if transfer.ReversalOfTransferID != nil {
		reversalOfTransferID = *transfer.ReversalOfTransferID
	}
	return transfer.InstitutionID == input.InstitutionID &&
		transfer.Provider == input.Provider &&
		transfer.ProviderReference == input.ProviderReference &&
		transfer.Direction == input.Direction &&
		transfer.AccountID == input.AccountID &&
		transfer.AmountMinor == input.AmountMinor &&
		transfer.CurrencyID == input.CurrencyID &&
		reversalOfTransferID == input.ReversalOfTransferID
}
