package corebanking

import (
	"database/sql"
	"errors"
)

func normalizeSQLError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
