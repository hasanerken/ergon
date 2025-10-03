
package postgres

import (
	"github.com/hasanerken/ergon"
	"context"
	"database/sql"
)

// Tx wraps a PostgreSQL transaction
type Tx struct {
	tx *sql.Tx
}

// Commit commits the transaction
func (t *Tx) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

// Rollback rolls back the transaction
func (t *Tx) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

// BeginTx begins a new transaction
func (s *Store) BeginTx(ctx context.Context) (ergon.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}
