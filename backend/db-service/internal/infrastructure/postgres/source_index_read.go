package postgres

import (
	"context"
	"fmt"
	"strings"
	"uuid"

	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/jackc/pgx/v5"
)

// Read каждый раз перепроверяет доступ; у запрещённых ID нет ничего, кроме самого ID.
func (r *IndexRepo) Read(ctx context.Context, in indexdoc.Read) (indexdoc.States, error) {
	out := indexdoc.States{}
	if strings.TrimSpace(in.ReaderID) == "" || len(in.ReaderID) > 256 || len(in.DocumentIDs) > 256 {
		return out, indexdoc.ErrInvalid
	}
	if in.SnapshotID != "" {
		if in.AccessOnly {
			return out, indexdoc.ErrInvalid
		}
		if _, err := uuid.Parse(in.SnapshotID); err != nil || len(in.DocumentIDs) != 1 {
			return out, indexdoc.ErrInvalid
		}
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("begin index access read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = tx.QueryRow(ctx, `SELECT value FROM source_corpus_epoch WHERE id=true`).Scan(&out.Epoch); err != nil {
		return out, fmt.Errorf("read source access epoch: %w", err)
	}
	if in.AccessOnly {
		out.Documents, err = readSourceAccess(ctx, tx, in)
		if err != nil {
			return out, err
		}
		if err = tx.Commit(ctx); err != nil {
			return out, fmt.Errorf("commit source access read: %w", err)
		}
		return out, nil
	}
	active, err := querySourceStates(ctx, tx, in, &out)
	if err != nil {
		return out, err
	}
	if err = attachIndexManifests(ctx, tx, in.SnapshotID, &out, active); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit source states read: %w", err)
	}
	return out, nil
}

func readSourceState(row pgx.Row, in indexdoc.Read) (indexdoc.State, string, error) {
	var state indexdoc.State
	var sourceRaw []byte
	var snapshotID *string
	var revoked bool
	if err := row.Scan(&state.DocumentID, &state.OwnerID, &sourceRaw, &snapshotID, &revoked); err != nil {
		return state, "", fmt.Errorf("scan source access: %w", err)
	}
	if len(sourceRaw) > 0 {
		source, err := sourcedoc.Decode(sourceRaw)
		if err != nil {
			return state, "", fmt.Errorf("decode source access: %w", err)
		}
		state.Source = source
	}
	state.Allowed = indexdoc.CanRead(in.ReaderID, state.OwnerID, state.Source, in.SharedCorpus, revoked)
	if !state.Allowed {
		return indexdoc.State{DocumentID: state.DocumentID}, "", nil
	}
	if snapshotID != nil {
		return state, *snapshotID, nil
	}
	return state, "", nil
}

func attachIndexManifests(
	ctx context.Context, tx pgx.Tx, requested string, out *indexdoc.States, active map[string]string,
) error {
	ids := make([]string, 0, len(out.Documents))
	snapshots := make([]string, 0, len(out.Documents))
	positions := make(map[string]int, len(out.Documents))
	for i := range out.Documents {
		state := &out.Documents[i]
		if !state.Allowed {
			continue
		}
		id := active[state.DocumentID]
		if requested != "" {
			id = requested
		}
		if id != "" {
			ids = append(ids, state.DocumentID)
			snapshots = append(snapshots, id)
			positions[state.DocumentID] = i
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT s.document_id,s.manifest
FROM unnest($1::text[], $2::text[]) requested(document_id,snapshot_id)
JOIN source_index_snapshots s USING(document_id,snapshot_id) WHERE s.ready`, ids, snapshots)
	if err != nil {
		return fmt.Errorf("read source manifests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return fmt.Errorf("scan source manifest: %w", err)
		}
		manifest, decodeErr := indexdoc.Decode[indexdoc.Manifest](raw)
		if decodeErr != nil {
			return decodeErr
		}
		if err = manifest.Validate(); err != nil {
			return err
		}
		out.Documents[positions[id]].Active = &manifest
		delete(positions, id)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("scan source manifests: %w", err)
	}
	if len(positions) != 0 {
		return indexdoc.ErrNotFound
	}
	return nil
}

// readSourceAccess не извлекает содержимое JSON: списку нужны только ID и решение.
func readSourceAccess(ctx context.Context, tx pgx.Tx, in indexdoc.Read) ([]indexdoc.State, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,
document_readable($2,d.owner_id,d.source_metadata,$3,coalesce(h.revoked,false))
FROM documents d LEFT JOIN source_index_heads h ON h.document_id=d.id
WHERE d.id=ANY($1)`, in.DocumentIDs, in.ReaderID, in.SharedCorpus)
	if err != nil {
		return nil, fmt.Errorf("read source access decisions: %w", err)
	}
	defer rows.Close()
	states := make([]indexdoc.State, 0, len(in.DocumentIDs))
	for rows.Next() {
		var state indexdoc.State
		if err = rows.Scan(&state.DocumentID, &state.Allowed); err != nil {
			return nil, fmt.Errorf("scan source access decision: %w", err)
		}
		states = append(states, state)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan source access decisions: %w", err)
	}
	return states, nil
}

func querySourceStates(ctx context.Context, tx pgx.Tx, in indexdoc.Read, out *indexdoc.States) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,d.owner_id,d.source_metadata,h.active_snapshot_id,coalesce(h.revoked,false)
FROM documents d LEFT JOIN source_index_heads h ON h.document_id=d.id WHERE d.id=ANY($1)`, in.DocumentIDs)
	if err != nil {
		return nil, fmt.Errorf("read source access: %w", err)
	}
	defer rows.Close()
	active := map[string]string{}
	for rows.Next() {
		state, snapshotID, scanErr := readSourceState(rows, in)
		if scanErr != nil {
			return nil, scanErr
		}
		active[state.DocumentID] = snapshotID
		out.Documents = append(out.Documents, state)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan source states: %w", err)
	}
	rows.Close()

	return active, nil
}
