package corebanking

import "github.com/jmoiron/sqlx"

type SQLRepository struct {
	db *sqlx.DB

	*sqlDemoRepository
	*sqlAccountRepository
	*sqlLedgerRepository
	*sqlTransferRepository
}

type SQLStore = SQLRepository

func NewSQLRepository(db *sqlx.DB) *SQLRepository {
	ledger := &sqlLedgerRepository{db: db}
	holds := &sqlHoldRepository{}
	providerEvents := &sqlProviderEventRepository{}
	return &SQLRepository{
		db:                    db,
		sqlDemoRepository:     &sqlDemoRepository{db: db},
		sqlAccountRepository:  &sqlAccountRepository{db: db},
		sqlLedgerRepository:   ledger,
		sqlTransferRepository: &sqlTransferRepository{db: db, ledger: ledger, holds: holds, providerEvents: providerEvents},
	}
}

func NewSQLStore(db *sqlx.DB) *SQLRepository {
	return NewSQLRepository(db)
}
