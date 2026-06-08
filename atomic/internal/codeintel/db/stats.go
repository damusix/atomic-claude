package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// GetStats returns aggregate counts for the current state of the index.
// It performs four COUNT queries and one MAX(indexed_at) query — all cheap
// scans. The NodesByKind map is populated from a GROUP BY query.
func (d *DB) GetStats(ctx context.Context) (types.GraphStats, error) {
	var s types.GraphStats

	// Node count.
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&s.NodeCount); err != nil {
		return s, fmt.Errorf("codeintel/db: GetStats nodeCount: %w", err)
	}

	// Edge count.
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&s.EdgeCount); err != nil {
		return s, fmt.Errorf("codeintel/db: GetStats edgeCount: %w", err)
	}

	// File count.
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&s.FileCount); err != nil {
		return s, fmt.Errorf("codeintel/db: GetStats fileCount: %w", err)
	}

	// Nodes by kind.
	rows, err := d.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM nodes GROUP BY kind`)
	if err != nil {
		return s, fmt.Errorf("codeintel/db: GetStats nodesByKind: %w", err)
	}
	defer rows.Close()

	s.NodesByKind = make(map[types.NodeKind]int)
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return s, fmt.Errorf("codeintel/db: GetStats nodesByKind scan: %w", err)
		}
		s.NodesByKind[types.NodeKind(kind)] = count
	}
	if err := rows.Err(); err != nil {
		return s, fmt.Errorf("codeintel/db: GetStats nodesByKind rows: %w", err)
	}

	// Last indexed at (MAX of all file indexed_at timestamps).
	var lastIndexedAt sql.NullString
	if err := d.db.QueryRowContext(ctx, `SELECT MAX(indexed_at) FROM files`).Scan(&lastIndexedAt); err != nil {
		return s, fmt.Errorf("codeintel/db: GetStats lastIndexedAt: %w", err)
	}
	if lastIndexedAt.Valid {
		s.LastIndexedAt = lastIndexedAt.String
	}

	return s, nil
}

// GetAllFiles returns all file records from the files table.
func (d *DB) GetAllFiles(ctx context.Context) ([]types.FileRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT path, content_hash, language, size, modified_at, indexed_at, node_count, errors
		FROM files
		ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetAllFiles: %w", err)
	}
	defer rows.Close()

	var files []types.FileRecord
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, fmt.Errorf("codeintel/db: GetAllFiles scan: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// Clear removes all data from nodes, edges, files, and unresolved_refs tables.
// The schema and project_metadata rows are preserved. Use after Uninitialize
// is inappropriate — use db.Close + os.RemoveAll for that case. Clear is for
// a "wipe and re-index" reset without re-creating the DB.
func (d *DB) Clear(ctx context.Context) error {
	return d.WithTx(ctx, func(tx *Tx) error {
		for _, table := range []string{"edges", "unresolved_refs", "nodes", "files"} {
			if _, err := tx.tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				return fmt.Errorf("codeintel/db: Clear %s: %w", table, err)
			}
		}
		return nil
	})
}
