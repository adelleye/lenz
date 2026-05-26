package corebanking

import (
	"context"
	"database/sql"
	"fmt"
)

func execOneRow(ctx context.Context, runner TxRunner, operation, query string, args ...any) error {
	result, err := runner.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	return requireOneRow(result, operation)
}

func requireOneRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrDataIntegrity, operation, rows)
	}
	return nil
}
