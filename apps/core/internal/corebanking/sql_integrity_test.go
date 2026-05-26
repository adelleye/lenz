package corebanking

import (
	"errors"
	"testing"
)

type rowsAffectedResult int64

func (r rowsAffectedResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r rowsAffectedResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

func TestRequireOneRow(t *testing.T) {
	if err := requireOneRow(rowsAffectedResult(1), "test update"); err != nil {
		t.Fatalf("expected one row to pass, got %v", err)
	}
	for _, rows := range []int64{0, 2} {
		if err := requireOneRow(rowsAffectedResult(rows), "test update"); !errors.Is(err, ErrDataIntegrity) {
			t.Fatalf("expected %d rows to return data integrity error, got %v", rows, err)
		}
	}
}
