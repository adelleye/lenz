package corebanking

import "github.com/jmoiron/sqlx"

type SQLRepository struct {
	db *sqlx.DB

	*sqlDemoRepository
	*sqlCustomerRepository
	*sqlAccountRepository
	*sqlLedgerRepository
	*sqlTransferRepository
}

func NewRepository(db *sqlx.DB) Repository {
	return newSQLRepository(db)
}

func newSQLRepository(db *sqlx.DB) *SQLRepository {
	ledger := &sqlLedgerRepository{db: db}
	holds := &sqlHoldRepository{}
	providerEvents := &sqlProviderEventRepository{}
	return &SQLRepository{
		db:                    db,
		sqlDemoRepository:     &sqlDemoRepository{db: db},
		sqlCustomerRepository: &sqlCustomerRepository{db: db},
		sqlAccountRepository:  &sqlAccountRepository{db: db},
		sqlLedgerRepository:   ledger,
		sqlTransferRepository: &sqlTransferRepository{db: db, ledger: ledger, holds: holds, providerEvents: providerEvents},
	}
}
