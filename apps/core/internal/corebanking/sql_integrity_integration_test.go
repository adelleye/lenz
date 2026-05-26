//go:build integration

package corebanking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

func TestSQLBalanceRowsAffectedIntegrityIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	t.Run("ledger_posting_missing_balance_rolls_back_journal_postings_and_balance", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		repo := newSQLRepository(db)
		account := createSQLCustomerAccount(t, svc, ctx, "Integrity", "Ledger", "integrity.ledger@example.com", "8334567801", "Integrity Ledger")
		clearing, err := svc.repository.GetDefaultInternalSettlementAccount(ctx, DemoInstitutionID, "NGN")
		if err != nil {
			t.Fatal(err)
		}
		deleteSQLBalanceRow(t, ctx, db, account.ID)
		before := readSQLIntegrityCounts(t, ctx, db)

		err = WithTx(ctx, db, func(tx TxRunner) error {
			_, err := repo.sqlLedgerRepository.postJournal(ctx, tx, RecordTransferInput{
				InstitutionID:     DemoInstitutionID,
				AccountID:         account.ID,
				ClearingAccountID: clearing.ID,
				Direction:         TransferDirectionInbound,
				Status:            TransferStatusSucceeded,
				AmountMinor:       1000,
				CurrencyID:        "NGN",
				IdempotencyKey:    "sql-integrity-ledger-post",
				Provider:          ProviderLedgerInternal,
				ProviderStatus:    TransferStatusSucceeded,
				Narration:         "Integrity ledger post",
			}, uuid.Must(uuid.NewRandom()).String(), time.Now().UTC(), postingBalanceOptions{})
			return err
		})

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
		assertSQLAccountBalancePair(t, svc, ctx, clearing.ID, 0, 0)
	})

	t.Run("internal_credit_missing_balance_fails_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		account := createSQLCustomerAccount(t, svc, ctx, "Integrity", "Credit", "integrity.credit@example.com", "8334567802", "Integrity Credit")
		deleteSQLBalanceRow(t, ctx, db, account.ID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.InternalCredit(ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    1000,
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-integrity-credit",
		})

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
	})

	t.Run("internal_debit_missing_balance_fails_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		account := createSQLCustomerAccount(t, svc, ctx, "Integrity", "Debit", "integrity.debit@example.com", "8334567803", "Integrity Debit")
		mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 5000, CurrencyID: "NGN", IdempotencyKey: "sql-integrity-debit-fund"})
		deleteSQLBalanceRow(t, ctx, db, account.ID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.InternalDebit(ctx, InternalDebitInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    1000,
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-integrity-debit",
		})

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
	})

	t.Run("internal_transfer_missing_destination_balance_fails_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		source := createSQLCustomerAccount(t, svc, ctx, "Integrity", "TransferSource", "integrity.transfer.source@example.com", "8334567804", "Integrity Transfer Source")
		destination := createSQLCustomerAccount(t, svc, ctx, "Integrity", "TransferDestination", "integrity.transfer.destination@example.com", "8334567805", "Integrity Transfer Destination")
		mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 5000, CurrencyID: "NGN", IdempotencyKey: "sql-integrity-transfer-fund"})
		deleteSQLBalanceRow(t, ctx, db, destination.ID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.InternalTransfer(ctx, InternalTransferInput{
			InstitutionID:        DemoInstitutionID,
			SourceAccountID:      source.ID,
			DestinationAccountID: destination.ID,
			AmountMinor:          1000,
			CurrencyID:           "NGN",
			IdempotencyKey:       "sql-integrity-transfer",
		})

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
	})

	t.Run("external_outbound_success_missing_balance_keeps_pending_hold_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-integrity-external-success-fund"})
		pending, intent := beginSQLExternalOutboundIntent(t, svc, ctx, "sql-integrity-external-success", 3000)
		deleteSQLBalanceRow(t, ctx, db, DemoCustomerAccountID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.repository.CompleteExternalOutboundTransfer(ctx, pending.ID, completeSQLExternalOutboundInput(intent, TransferStatusSucceeded))

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
		assertStatus(t, mustGetSQLTransfer(t, svc, ctx, pending.ID), TransferStatusPending)
		assertSQLTransferHold(t, ctx, db, pending.ID, HoldStatusActive)
	})

	t.Run("hold_release_missing_balance_rolls_back_hold_update_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-integrity-hold-release-fund"})
		pending, intent := beginSQLExternalOutboundIntent(t, svc, ctx, "sql-integrity-hold-release", 3000)
		deleteSQLBalanceRow(t, ctx, db, DemoCustomerAccountID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.repository.CompleteExternalOutboundTransfer(ctx, pending.ID, completeSQLExternalOutboundInput(intent, TransferStatusFailed))

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
		assertStatus(t, mustGetSQLTransfer(t, svc, ctx, pending.ID), TransferStatusPending)
		assertSQLTransferHold(t, ctx, db, pending.ID, HoldStatusActive)
	})

	t.Run("lien_place_missing_balance_fails_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		account := createSQLCustomerAccount(t, svc, ctx, "Integrity", "LienPlace", "integrity.lien.place@example.com", "8334567806", "Integrity Lien Place")
		mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-integrity-lien-place-fund"})
		deleteSQLBalanceRow(t, ctx, db, account.ID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.PlaceAccountLien(ctx, AccountLienInput{
			InstitutionID: DemoInstitutionID,
			AccountID:     account.ID,
			AmountMinor:   2000,
			CurrencyID:    "NGN",
			Reference:     "sql-integrity-lien-place",
			Reason:        "integrity check",
		})

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
	})

	t.Run("lien_release_missing_balance_rolls_back_lien_update_without_effects", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		account := createSQLCustomerAccount(t, svc, ctx, "Integrity", "LienRelease", "integrity.lien.release@example.com", "8334567807", "Integrity Lien Release")
		mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-integrity-lien-release-fund"})
		lien := mustPlaceSQLLien(t, svc, ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 2000, CurrencyID: "NGN", Reference: "sql-integrity-lien-release", Reason: "integrity check"})
		deleteSQLBalanceRow(t, ctx, db, account.ID)
		before := readSQLIntegrityCounts(t, ctx, db)

		_, err := svc.ReleaseAccountLien(ctx, ReleaseLienInput{
			InstitutionID: DemoInstitutionID,
			AccountID:     account.ID,
			LienID:        lien.ID,
			Reference:     "sql-integrity-lien-release-clear",
			Reason:        "integrity clear",
		})

		assertDataIntegrityError(t, err)
		assertSQLIntegrityCounts(t, ctx, db, before)
		assertSQLLienRow(t, ctx, db, lien.ID, HoldStatusActive, "sql-integrity-lien-release")
	})
}

func beginSQLExternalOutboundIntent(t *testing.T, svc *Service, ctx context.Context, idempotencyKey string, amountMinor int64) (*Transfer, RecordTransferInput) {
	t.Helper()
	clearing, err := svc.repository.GetDefaultInternalSettlementAccount(ctx, DemoInstitutionID, "NGN")
	if err != nil {
		t.Fatal(err)
	}
	input := RecordTransferInput{
		InstitutionID:      DemoInstitutionID,
		AccountID:          DemoCustomerAccountID,
		ClearingAccountID:  clearing.ID,
		Direction:          TransferDirectionOutbound,
		Status:             TransferStatusPending,
		AmountMinor:        amountMinor,
		CurrencyID:         "NGN",
		IdempotencyKey:     idempotencyKey,
		Provider:           ProviderMockNIP,
		ProviderReference:  idempotencyKey + "-ref",
		ProviderStatus:     TransferStatusPending,
		RequestFingerprint: idempotencyKey + "-fingerprint",
		Narration:          "Integrity external outbound",
		RejectInsufficient: true,
		RequireAvailable:   true,
	}
	transfer, created, err := svc.repository.BeginExternalOutboundTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !created || transfer.Status != TransferStatusPending {
		t.Fatalf("expected new pending external outbound intent, created=%t transfer=%+v", created, transfer)
	}
	return transfer, input
}

func completeSQLExternalOutboundInput(intent RecordTransferInput, status string) RecordTransferInput {
	intent.Status = status
	intent.ProviderStatus = status
	intent.ProviderEventID = intent.IdempotencyKey + "-event"
	if status == TransferStatusFailed {
		intent.FailureReason = "provider_failed"
	}
	return intent
}

type sqlIntegrityCounts struct {
	Transfers      int
	ProviderEvents int
	Journals       int
	Postings       int
	Balances       int
	Holds          int
	Audits         int
}

func readSQLIntegrityCounts(t *testing.T, ctx context.Context, db *sqlx.DB) sqlIntegrityCounts {
	t.Helper()
	var counts sqlIntegrityCounts
	readSQLCount(t, ctx, db, "transfers", &counts.Transfers)
	readSQLCount(t, ctx, db, "provider_events", &counts.ProviderEvents)
	readSQLCount(t, ctx, db, "journal_entries", &counts.Journals)
	readSQLCount(t, ctx, db, "postings", &counts.Postings)
	readSQLCount(t, ctx, db, "account_balances", &counts.Balances)
	readSQLCount(t, ctx, db, "account_holds", &counts.Holds)
	readSQLCount(t, ctx, db, "audit_events", &counts.Audits)
	return counts
}

func readSQLCount(t *testing.T, ctx context.Context, db *sqlx.DB, table string, dest *int) {
	t.Helper()
	if err := db.GetContext(ctx, dest, "SELECT COUNT(*) FROM "+table); err != nil {
		t.Fatal(err)
	}
}

func assertSQLIntegrityCounts(t *testing.T, ctx context.Context, db *sqlx.DB, want sqlIntegrityCounts) {
	t.Helper()
	if got := readSQLIntegrityCounts(t, ctx, db); got != want {
		t.Fatalf("SQL side effects changed: got %+v want %+v", got, want)
	}
}

func deleteSQLBalanceRow(t *testing.T, ctx context.Context, db *sqlx.DB, accountID string) {
	t.Helper()
	result, err := db.ExecContext(ctx, `DELETE FROM account_balances WHERE institution_id = $1 AND account_id = $2`, DemoInstitutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireOneRow(result, "delete test account balance"); err != nil {
		t.Fatal(err)
	}
}

func assertDataIntegrityError(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrDataIntegrity) {
		t.Fatalf("expected data integrity error, got %v", err)
	}
}
