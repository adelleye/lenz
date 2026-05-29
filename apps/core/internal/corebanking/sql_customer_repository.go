package corebanking

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type sqlCustomerRepository struct {
	db *sqlx.DB
}

const customerSelectSQL = `SELECT
	id,
	institution_id,
	branch_id,
	COALESCE(NULLIF(meta->>'customer_type', ''), 'individual') AS customer_type,
	first_name,
	last_name,
	NULLIF(meta->>'business_name', '') AS business_name,
	email,
	phone,
	status,
	COALESCE(NULLIF(meta->>'kyc_tier', ''), 'tier1') AS kyc_tier,
	COALESCE(NULLIF(meta->>'bvn_status', ''), 'not_collected') AS bvn_status,
	COALESCE(NULLIF(meta->>'nin_status', ''), 'not_collected') AS nin_status,
	created_at,
	updated_at
FROM customers`

type customerMeta struct {
	CustomerType string `json:"customer_type"`
	BusinessName string `json:"business_name,omitempty"`
	KYCTier      string `json:"kyc_tier"`
	BVNStatus    string `json:"bvn_status"`
	NINStatus    string `json:"nin_status"`
}

func (r *sqlCustomerRepository) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	var customer Customer
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		var branchExists bool
		if err := tx.GetContext(ctx, &branchExists, `SELECT EXISTS (SELECT 1 FROM branches WHERE institution_id = $1 AND id = $2)`, input.InstitutionID, input.BranchID); err != nil {
			return normalizeSQLError(err)
		}
		if !branchExists {
			return ErrNotFound
		}

		now := time.Now().UTC()
		meta, err := json.Marshal(customerMeta{
			CustomerType: input.CustomerType,
			BusinessName: input.BusinessName,
			KYCTier:      input.kycTier,
			BVNStatus:    input.bvnStatus,
			NINStatus:    input.ninStatus,
		})
		if err != nil {
			return err
		}
		if err := normalizeSQLError(tx.GetContext(ctx, &customer, `
INSERT INTO customers (id, institution_id, branch_id, first_name, last_name, email, phone, status, meta, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8::jsonb, $9, $9)
RETURNING
	id,
	institution_id,
	branch_id,
	COALESCE(NULLIF(meta->>'customer_type', ''), 'individual') AS customer_type,
	first_name,
	last_name,
	NULLIF(meta->>'business_name', '') AS business_name,
	email,
	phone,
	status,
	COALESCE(NULLIF(meta->>'kyc_tier', ''), 'tier1') AS kyc_tier,
	COALESCE(NULLIF(meta->>'bvn_status', ''), 'not_collected') AS bvn_status,
	COALESCE(NULLIF(meta->>'nin_status', ''), 'not_collected') AS nin_status,
	created_at,
	updated_at`,
			uuid.Must(uuid.NewRandom()).String(),
			input.InstitutionID,
			input.BranchID,
			input.FirstName,
			input.LastName,
			input.Email,
			input.Phone,
			string(meta),
			now,
		)); err != nil {
			return err
		}
		_, err = insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID: customer.InstitutionID,
			Action:        AuditActionCustomerCreated,
			EntityType:    "customer",
			EntityID:      customer.ID,
			CustomerID:    customer.ID,
			NewStatus:     customer.Status,
			Metadata: map[string]string{
				"customer_type": customer.CustomerType,
				"branch_id":     customer.BranchID,
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &customer, nil
}

func (r *sqlCustomerRepository) GetCustomer(ctx context.Context, institutionID, customerID string) (*Customer, error) {
	var customer Customer
	err := r.db.GetContext(ctx, &customer, customerSelectSQL+` WHERE institution_id = $1 AND id = $2`, institutionID, customerID)
	return &customer, normalizeSQLError(err)
}
