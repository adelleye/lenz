package corebanking

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

type sqlDemoRepository struct {
	db *sqlx.DB
}

func (r *sqlDemoRepository) EnsureDemoData(ctx context.Context) (*SeedResult, error) {
	if err := WithTx(ctx, r.db, func(tx TxRunner) error {
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `
INSERT INTO currencies (id, name, created_at, updated_at)
VALUES ('NGN', 'Nigerian Naira', $1, $1)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = EXCLUDED.updated_at`, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO countries (id, name, flag, currency, is_supported, meta, created_at, updated_at)
VALUES ('NG', 'Nigeria', 'NG', 'NGN', true, '{}'::jsonb, $1, $1)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, currency = EXCLUDED.currency, is_supported = EXCLUDED.is_supported, updated_at = EXCLUDED.updated_at`, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO institutions (id, name, short_name, code, nuban_prefix, country_id, currency_id, status, meta, created_at, updated_at)
VALUES ($1, 'Lenz Demo Microfinance Bank', 'Lenz Demo', '999001', '999', 'NG', 'NGN', 'active', '{}'::jsonb, $2, $2)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, short_name = EXCLUDED.short_name, code = EXCLUDED.code, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			DemoInstitutionID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO branches (id, institution_id, code, name, meta, status, created_at, updated_at)
VALUES ($1, $2, 'HQ', 'Demo HQ', '{}'::jsonb, 'active', $3, $3)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			DemoBranchID, DemoInstitutionID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO customers (id, institution_id, branch_id, first_name, last_name, email, phone, status, meta, created_at, updated_at)
VALUES ($1, $2, $3, 'Ada', 'Demo', 'ada.demo@example.com', '+2348000000000', 'active', '{"customer_type":"individual","kyc_tier":"tier1","bvn_status":"not_collected","nin_status":"not_collected"}'::jsonb, $4, $4)
ON CONFLICT (id) DO UPDATE SET first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name, email = EXCLUDED.email, phone = EXCLUDED.phone, status = EXCLUDED.status, meta = EXCLUDED.meta, updated_at = EXCLUDED.updated_at`,
			DemoCustomerID, DemoInstitutionID, DemoBranchID, now); err != nil {
			return err
		}
		customerID := DemoCustomerID
		if _, err := tx.ExecContext(ctx, `
INSERT INTO accounts (id, institution_id, customer_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status, created_at, updated_at)
VALUES ($1, $2, $3, '9990000001', 'Ada Demo Wallet', 'customer', 'standard_wallet', false, 'NGN', 'credit', 'active', $4, $4)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, product_type = EXCLUDED.product_type, allow_negative_balance = EXCLUDED.allow_negative_balance, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			DemoCustomerAccountID, DemoInstitutionID, customerID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO accounts (id, institution_id, customer_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status, created_at, updated_at)
VALUES ($1, $2, NULL, '9999999999', 'Mock NIP Clearing', 'internal', 'internal', true, 'NGN', 'debit', 'active', $3, $3)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, product_type = EXCLUDED.product_type, allow_negative_balance = EXCLUDED.allow_negative_balance, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			DemoClearingAccountID, DemoInstitutionID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO account_balances (account_id, institution_id, available_minor, ledger_minor, currency_id, updated_at)
VALUES ($1, $2, 0, 0, 'NGN', $3)
ON CONFLICT (account_id) DO NOTHING`, DemoCustomerAccountID, DemoInstitutionID, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO account_balances (account_id, institution_id, available_minor, ledger_minor, currency_id, updated_at)
VALUES ($1, $2, 0, 0, 'NGN', $3)
ON CONFLICT (account_id) DO NOTHING`, DemoClearingAccountID, DemoInstitutionID, now)
		return err
	}); err != nil {
		return nil, err
	}

	return r.seedResult(ctx)
}

func (r *sqlDemoRepository) seedResult(ctx context.Context) (*SeedResult, error) {
	var out SeedResult
	if err := r.db.GetContext(ctx, &out.Institution, `SELECT id, name, short_name, code, currency_id, status, created_at, updated_at FROM institutions WHERE id = $1`, DemoInstitutionID); err != nil {
		return nil, err
	}
	if err := r.db.GetContext(ctx, &out.Branch, `SELECT id, institution_id, code, name, status, created_at, updated_at FROM branches WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoBranchID); err != nil {
		return nil, err
	}
	if err := r.db.GetContext(ctx, &out.Customer, customerSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoCustomerID); err != nil {
		return nil, err
	}
	if err := r.db.GetContext(ctx, &out.Account, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoCustomerAccountID); err != nil {
		return nil, err
	}
	if err := r.db.GetContext(ctx, &out.Clearing, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoClearingAccountID); err != nil {
		return nil, err
	}
	return &out, nil
}
