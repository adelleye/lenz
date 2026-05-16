package institution

import "github.com/jmoiron/sqlx"

type Repository interface{}

type basicRepository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &basicRepository{db: db}
}
