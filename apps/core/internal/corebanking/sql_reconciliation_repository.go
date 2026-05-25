package corebanking

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const reconciliationItemSelectSQL = `
SELECT
    id AS transfer_id,
    institution_id,
    account_id,
    direction,
    status,
    provider,
    provider_reference,
    provider_event_id,
    provider_status,
    ledger_status,
    reconciliation_status,
    amount_minor,
    currency_id,
    failure_reason,
    journal_entry_id,
    created_at,
    updated_at,
    review_status,
    review_note,
    reviewed_at,
    reviewed_by
FROM transfers`

func (r *sqlTransferRepository) ListReconciliationItems(ctx context.Context, institutionID string, options ListReconciliationItemsOptions) ([]ReconciliationItem, error) {
	cutoff := reconciliationStalePendingCutoff(options)
	args := []any{institutionID, cutoff}
	clauses := []string{
		"institution_id = $1",
		reconciliationIssueSQL("$2"),
		"(review_status IS NULL OR review_status = 'manual_followup_required')",
	}
	addFilter := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		args = append(args, strings.TrimSpace(value))
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addFilter("status", options.Status)
	addFilter("provider_status", options.ProviderStatus)
	addFilter("ledger_status", options.LedgerStatus)
	addFilter("reconciliation_status", options.ReconciliationStatus)
	if options.BeforeCreatedAt != nil && !options.BeforeCreatedAt.IsZero() {
		args = append(args, *options.BeforeCreatedAt)
		beforeCreatedAtArg := len(args)
		if strings.TrimSpace(options.BeforeTransferID) != "" {
			args = append(args, strings.TrimSpace(options.BeforeTransferID))
			clauses = append(clauses, fmt.Sprintf("(created_at < $%d OR (created_at = $%d AND id < $%d::uuid))", beforeCreatedAtArg, beforeCreatedAtArg, len(args)))
		} else {
			clauses = append(clauses, fmt.Sprintf("created_at < $%d", beforeCreatedAtArg))
		}
	}
	args = append(args, options.Limit)
	query := reconciliationItemSelectSQL + `
WHERE ` + strings.Join(clauses, "\n  AND ") + fmt.Sprintf(`
ORDER BY created_at DESC, id DESC
LIMIT $%d`, len(args))

	var items []ReconciliationItem
	if err := r.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, err
	}
	decorateReconciliationItems(items, cutoff)
	return items, nil
}

func (r *sqlTransferRepository) GetReconciliationItem(ctx context.Context, institutionID, transferID string) (*ReconciliationItem, error) {
	cutoff := reconciliationStalePendingCutoff(ListReconciliationItemsOptions{})
	item, err := r.getReconciliationItem(ctx, r.db, institutionID, transferID, false)
	if err != nil {
		return nil, err
	}
	decorateReconciliationItem(item, cutoff)
	if !isInspectableReconciliationItem(*item, cutoff) {
		return nil, ErrNotFound
	}
	return item, nil
}

func (r *sqlTransferRepository) MarkReconciliationItemReviewed(ctx context.Context, input MarkReconciliationItemReviewedInput) (*ReconciliationItem, error) {
	cutoff := reconciliationStalePendingCutoff(ListReconciliationItemsOptions{})
	var item *ReconciliationItem
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		current, err := r.getReconciliationItem(ctx, tx, input.InstitutionID, input.TransferID, true)
		if err != nil {
			return err
		}
		decorateReconciliationItem(current, cutoff)
		if !isInspectableReconciliationItem(*current, cutoff) {
			return ErrNotFound
		}

		_, reviewedBy, _, _ := auditContext(ctx)
		now := time.Now().UTC()
		if _, err = tx.ExecContext(ctx, `
UPDATE transfers
SET review_status = $1,
    review_note = $2,
    reviewed_at = $3,
    reviewed_by = $4,
    updated_at = $3
WHERE institution_id = $5 AND id = $6`,
			input.ResolutionStatus,
			input.ResolutionNote,
			now,
			reviewedBy,
			input.InstitutionID,
			input.TransferID,
		); err != nil {
			return err
		}
		if _, err = insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID:  current.InstitutionID,
			Action:         AuditActionReconciliationReviewed,
			EntityType:     "transfer",
			EntityID:       current.TransferID,
			AccountID:      current.AccountID,
			TransferID:     current.TransferID,
			JournalEntryID: optionalAuditValue(current.JournalEntryID),
			Reference:      current.ProviderReference,
			OldStatus:      optionalAuditValue(current.ReviewStatus),
			NewStatus:      input.ResolutionStatus,
			Metadata: map[string]string{
				"resolution_note":       input.ResolutionNote,
				"review_reason":         current.ReviewReason,
				"recommended_action":    current.RecommendedNextAction,
				"provider_status":       current.ProviderStatus,
				"ledger_status":         current.LedgerStatus,
				"reconciliation_status": current.ReconciliationStatus,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		item, err = r.getReconciliationItem(ctx, tx, input.InstitutionID, input.TransferID, false)
		if err != nil {
			return err
		}
		decorateReconciliationItem(item, cutoff)
		return nil
	})
	return item, err
}

func (r *sqlTransferRepository) getReconciliationItem(ctx context.Context, runner TxRunner, institutionID, transferID string, forUpdate bool) (*ReconciliationItem, error) {
	query := reconciliationItemSelectSQL + `
WHERE institution_id = $1 AND id = $2`
	if forUpdate {
		query += `
FOR UPDATE`
	}
	var item ReconciliationItem
	err := runner.GetContext(ctx, &item, query, institutionID, transferID)
	return &item, normalizeSQLError(err)
}

func reconciliationIssueSQL(cutoffArg string) string {
	return `(
    reconciliation_status = 'manual_review'
    OR provider_status = 'provider_unknown'
    OR ledger_status = 'reversal_deficit'
    OR (provider_status = 'succeeded' AND ledger_status <> 'posted')
    OR (ledger_status = 'posted' AND provider_status = 'failed')
    OR (status = 'pending' AND created_at < ` + cutoffArg + `)
)`
}

func reconciliationStalePendingCutoff(options ListReconciliationItemsOptions) time.Time {
	minutes := options.StalePendingMinutes
	if minutes <= 0 {
		minutes = DefaultReconciliationStalePendingMinutes
	}
	return time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
}

func decorateReconciliationItems(items []ReconciliationItem, stalePendingCutoff time.Time) {
	for i := range items {
		decorateReconciliationItem(&items[i], stalePendingCutoff)
	}
}

func decorateReconciliationItem(item *ReconciliationItem, stalePendingCutoff time.Time) {
	if item == nil {
		return
	}
	reason, action, _ := reconciliationReviewState(*item, stalePendingCutoff)
	item.ReviewReason = reason
	item.RecommendedNextAction = action
}

func isInspectableReconciliationItem(item ReconciliationItem, stalePendingCutoff time.Time) bool {
	if item.ReviewStatus != nil {
		return true
	}
	_, _, needsReview := reconciliationReviewState(item, stalePendingCutoff)
	return needsReview
}

func reconciliationReviewState(item ReconciliationItem, stalePendingCutoff time.Time) (string, string, bool) {
	switch {
	case item.LedgerStatus == LedgerStatusReversalDeficit:
		return "reversal_deficit", ReconciliationActionManualCustomerReceivableReview, true
	case item.ProviderStatus == TransferProviderStatusUnknown:
		return "provider_unknown", ReconciliationActionRequeryProvider, true
	case item.ProviderStatus == TransferStatusSucceeded && item.LedgerStatus != LedgerStatusPosted:
		return "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal, true
	case item.ProviderStatus == TransferStatusFailed && item.LedgerStatus == LedgerStatusPosted:
		return "provider_failed_ledger_posted", ReconciliationActionContactProvider, true
	case item.ReconciliationStatus == ReconciliationStatusManualReview:
		return "manual_review", ReconciliationActionInspectJournal, true
	case item.Status == TransferStatusPending && item.CreatedAt.Before(stalePendingCutoff):
		return "stale_pending", ReconciliationActionRequeryProvider, true
	default:
		return "none", ReconciliationActionNoAction, false
	}
}
