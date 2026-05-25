package corebanking

func customerInitiatedOutbound(input RecordTransferInput) bool {
	return input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == ""
}

func canUseAvailableBalance(account Account, availableMinor, amountMinor int64) bool {
	if account.AllowNegative {
		return true
	}
	if account.Kind != AccountKindCustomer {
		return true
	}
	return availableMinor >= amountMinor
}

func enforceTransferControls(input RecordTransferInput, account, clearing Account) error {
	if err := enforcePrimaryAccountControls(input, account); err != nil {
		return err
	}
	return enforceCounterpartyAccountControls(input, clearing)
}

func enforcePrimaryAccountControls(input RecordTransferInput, account Account) error {
	if account.Kind != AccountKindCustomer {
		return nil
	}
	switch account.Status {
	case AccountStatusFrozen, AccountStatusClosed:
		return ErrInvalidRequest
	case AccountStatusPostNoDebit:
		if input.Direction == TransferDirectionOutbound {
			return ErrInvalidRequest
		}
	}
	return nil
}

func enforceCounterpartyAccountControls(input RecordTransferInput, account Account) error {
	if account.Kind != AccountKindCustomer {
		return nil
	}
	if account.Status == AccountStatusFrozen || account.Status == AccountStatusClosed {
		return ErrInvalidRequest
	}
	return nil
}

func wouldCreateReversalDeficit(account Account, balance AccountBalance, input RecordTransferInput) bool {
	if input.ReversalOfTransferID == "" || input.Direction != TransferDirectionOutbound {
		return false
	}
	if account.Kind != AccountKindCustomer || account.AllowNegative {
		return false
	}
	return balance.LedgerMinor-input.AmountMinor < 0
}

func transferStatuses(status string) (ledgerStatus, reconciliationStatus string) {
	switch status {
	case TransferStatusSucceeded:
		return LedgerStatusPosted, ReconciliationStatusMatched
	case TransferStatusFailed:
		return LedgerStatusNoPosting, ReconciliationStatusNoAction
	default:
		return LedgerStatusPending, ReconciliationStatusPending
	}
}
