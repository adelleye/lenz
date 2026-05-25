package corebanking

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type sqlCustomerRepository struct {
	db *sqlx.DB
}

const customerSelectSQL = `SELECT id, institution_id, branch_id, first_name, last_name, email, phone, status, created_at, updated_at FROM customers`

func (r *sqlCustomerRepository) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	var customer Customer
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		var branchID string
		if err := tx.GetContext(ctx, &branchID, `SELECT id FROM branches WHERE institution_id = $1 AND id = $2`, input.InstitutionID, input.BranchID); err != nil {
			return normalizeSQLError(err)
		}

		now := time.Now().UTC()
		return tx.GetContext(ctx, &customer, `
INSERT INTO customers (id, institution_id, branch_id, first_name, last_name, email, phone, status, meta, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', '{}'::jsonb, $8, $8)
RETURNING id, institution_id, branch_id, first_name, last_name, email, phone, status, created_at, updated_at`,
			uuid.Must(uuid.NewRandom()).String(),
			input.InstitutionID,
			input.BranchID,
			input.FirstName,
			input.LastName,
			input.Email,
			input.Phone,
			now,
		)
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
