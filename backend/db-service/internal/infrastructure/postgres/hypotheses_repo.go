package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/example/db-service/internal/platform/contracts"
	"github.com/jackc/pgx/v5"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
)

type KPIRepo struct {
	q            *sqlcgen.Queries
	db           *DB
	sharedCorpus bool
}

func NewKPIRepo(db *DB) *KPIRepo { return &KPIRepo{q: sqlcgen.New(db.Pool), db: db} }

// WithSharedCorpus задаёт серверную политику, которую вызывающий RPC изменить не может.
func (r *KPIRepo) WithSharedCorpus(shared bool) *KPIRepo {
	r.sharedCorpus = shared
	return r
}

func kpiFromRow(k sqlcgen.Kpi) *domain.KPI {
	return &domain.KPI{
		ID: k.ID, OwnerID: k.OwnerID, Title: k.Title, Description: k.Description, Metric: k.Metric,
		Unit: k.Unit, Direction: k.Direction, Baseline: k.Baseline, Target: k.Target,
		FunctionArea: k.FunctionArea, Status: k.Status, Detail: k.Detail,
		CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
	}
}

func (r *KPIRepo) Create(ctx context.Context, k *domain.KPI) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	previous, err := lockHypothesisEffect(ctx, tx, k.OwnerID, "create-kpi")
	if err != nil {
		return err
	}
	if previous != "" {
		row, e := r.q.WithTx(tx).GetKPI(ctx, previous)
		if e != nil {
			return e
		}
		*k = *kpiFromRow(row)
		return nil
	}
	err = r.q.WithTx(tx).CreateKPI(ctx, sqlcgen.CreateKPIParams{
		ID: k.ID, OwnerID: k.OwnerID, Title: k.Title, Description: k.Description, Metric: k.Metric,
		Unit: k.Unit, Direction: k.Direction, Baseline: k.Baseline, Target: k.Target,
		FunctionArea: k.FunctionArea, Status: k.Status, Detail: jsonbOr(k.Detail, "{}"),
		CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert kpi: %w", err)
	}
	if err = saveHypothesisEffect(ctx, tx, "create-kpi", k.ID); err != nil {
		return err
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return fmt.Errorf("create: %w", operationErr)
	}
	return nil
}

func (r *KPIRepo) Get(ctx context.Context, id string) (*domain.KPI, error) {
	k, err := r.q.GetKPI(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select kpi: %w", err)
	}
	return kpiFromRow(k), nil
}

func (r *KPIRepo) ListByOwner(ctx context.Context, ownerID string) ([]*domain.KPI, error) {
	rows, err := r.q.ListKPIsByOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list kpis: %w", err)
	}
	out := make([]*domain.KPI, 0, len(rows))
	for i := range rows {
		out = append(out, kpiFromRow(rows[i]))
	}
	return out, nil
}

func (r *KPIRepo) Update(ctx context.Context, k *domain.KPI) error {
	n, err := r.q.UpdateKPI(ctx, sqlcgen.UpdateKPIParams{
		ID: k.ID, Title: k.Title, Description: k.Description, Metric: k.Metric, Unit: k.Unit,
		Direction: k.Direction, Baseline: k.Baseline, Target: k.Target, FunctionArea: k.FunctionArea,
		Status: k.Status, Detail: jsonbOr(k.Detail, "{}"),
	})
	if err != nil {
		return fmt.Errorf("update kpi: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *KPIRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteKPI(ctx, id)
	if err != nil {
		return fmt.Errorf("delete kpi: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AttachDocuments проверяет весь набор в снимке одной атомарной SQL-команды.
func (r *KPIRepo) AttachDocuments(ctx context.Context, readerID, kpiID string, documentIDs []string, role string) error {
	tag, err := r.db.Pool.Exec(ctx, `WITH requested AS (
  SELECT DISTINCT unnest($3::text[]) AS id
), readable AS (
  SELECT d.id FROM requested r JOIN documents d ON d.id=r.id
  LEFT JOIN source_index_heads h ON h.document_id=d.id
  WHERE document_readable($1,d.owner_id,d.source_metadata,$5,coalesce(h.revoked,false))
)
INSERT INTO kpi_documents(kpi_id,document_id,role)
SELECT $2,id,$4 FROM readable
WHERE EXISTS(SELECT 1 FROM kpis WHERE id=$2 AND owner_id=$1)
  AND (SELECT count(*) FROM readable)=(SELECT count(*) FROM requested)
ON CONFLICT(kpi_id,document_id) DO UPDATE SET role=EXCLUDED.role`,
		readerID, kpiID, documentIDs, role, r.sharedCorpus)
	if err != nil {
		return fmt.Errorf("attach kpi document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *KPIRepo) ListDocuments(ctx context.Context, readerID, kpiID string) ([]*domain.KPIDocumentLink, error) {
	rows, err := r.q.ListKpiDocuments(ctx, sqlcgen.ListKpiDocumentsParams{
		ReaderID: readerID, KpiID: kpiID, Shared: r.sharedCorpus,
	})
	if err != nil {
		return nil, fmt.Errorf("list kpi documents: %w", err)
	}
	out := make([]*domain.KPIDocumentLink, 0, len(rows))
	for i := range rows {
		out = append(out, &domain.KPIDocumentLink{
			Document:   documentFromRow(rows[i].Document),
			Role:       rows[i].Role,
			AttachedAt: rows[i].AttachedAt,
		})
	}
	return out, nil
}

func (r *KPIRepo) DetachDocument(ctx context.Context, kpiID, documentID string) error {
	err := r.q.DetachKpiDocument(ctx, sqlcgen.DetachKpiDocumentParams{KpiID: kpiID, DocumentID: documentID})
	if err != nil {
		return fmt.Errorf("detach kpi document: %w", err)
	}
	return nil
}

type ClusterRepo struct {
	q  *sqlcgen.Queries
	db *DB
}

func NewClusterRepo(db *DB) *ClusterRepo { return &ClusterRepo{q: sqlcgen.New(db.Pool), db: db} }

func clusterFromRow(c sqlcgen.Cluster) *domain.Cluster {
	return &domain.Cluster{
		ID: c.ID, OwnerID: c.OwnerID, Label: c.Label, Summary: c.Summary, Keywords: c.Keywords,
		Method: c.Method, ChunkCount: int(c.ChunkCount), DocumentCount: int(c.DocumentCount),
		Representatives: c.Representatives, Params: c.Params, Status: c.Status,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func (r *ClusterRepo) Create(ctx context.Context, c *domain.Cluster) error {
	err := r.q.CreateCluster(ctx, sqlcgen.CreateClusterParams{
		ID: c.ID, OwnerID: c.OwnerID, Label: c.Label, Summary: c.Summary, Keywords: orEmpty(c.Keywords),
		Method: c.Method, ChunkCount: int32(c.ChunkCount), DocumentCount: int32(c.DocumentCount),
		Representatives: jsonbOr(c.Representatives, "[]"), Params: jsonbOr(c.Params, "{}"), Status: c.Status,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert cluster: %w", err)
	}
	return nil
}

func (r *ClusterRepo) Get(ctx context.Context, id string) (*domain.Cluster, error) {
	c, err := r.q.GetCluster(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select cluster: %w", err)
	}
	return clusterFromRow(c), nil
}

func (r *ClusterRepo) ListByOwner(ctx context.Context, ownerID string) ([]*domain.Cluster, error) {
	rows, err := r.q.ListClustersByOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	out := make([]*domain.Cluster, 0, len(rows))
	for i := range rows {
		out = append(out, clusterFromRow(rows[i]))
	}
	return out, nil
}

func (r *ClusterRepo) Update(ctx context.Context, c *domain.Cluster) error {
	n, err := r.q.UpdateCluster(ctx, sqlcgen.UpdateClusterParams{
		ID: c.ID, Label: c.Label, Summary: c.Summary, Keywords: orEmpty(c.Keywords), Method: c.Method,
		ChunkCount: int32(c.ChunkCount), DocumentCount: int32(c.DocumentCount),
		Representatives: jsonbOr(c.Representatives, "[]"), Params: jsonbOr(c.Params, "{}"), Status: c.Status,
	})
	if err != nil {
		return fmt.Errorf("update cluster: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteByOwner: ссылки гипотез на кластеры обнуляет внешний ключ.
func (r *ClusterRepo) DeleteByOwner(ctx context.Context, ownerID string) (int64, error) {
	n, err := r.q.DeleteClustersByOwner(ctx, ownerID)
	if err != nil {
		return 0, fmt.Errorf("delete clusters: %w", err)
	}
	return n, nil
}

type HypothesisRepo struct {
	db *DB
	q  *sqlcgen.Queries
}

func NewHypothesisRepo(db *DB) *HypothesisRepo {
	return &HypothesisRepo{db: db, q: sqlcgen.New(db.Pool)}
}

func hypothesisFromRow(h sqlcgen.Hypothesis) *domain.Hypothesis {
	return &domain.Hypothesis{
		Version: h.Version,
		ID:      h.ID, OwnerID: h.OwnerID, RunID: h.RunID, Title: h.Title, Statement: h.Statement,
		Rationale: h.Rationale, Method: h.Method, Status: h.Status, KPIID: h.KpiID,
		PrimaryClusterID: h.PrimaryClusterID, TRL: i16ToIntPtr(h.Trl), NoveltyScore: h.NoveltyScore,
		RiskScore: h.RiskScore, ValueScore: h.ValueScore, ConfidenceScore: h.ConfidenceScore,
		CompositeScore: h.CompositeScore, Measurable: h.Measurable, Organization: h.Organization,
		FunctionArea: h.FunctionArea, SourceType: h.SourceType, Location: h.Location, Tags: h.Tags,
		Assessment: h.Assessment, Detail: h.Detail, Generation: h.Generation,
		CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

func evidenceFromRow(e sqlcgen.HypothesisEvidence) *domain.Evidence {
	return &domain.Evidence{
		ID: e.ID, HypothesisID: e.HypothesisID, DocumentID: e.DocumentID, ChunkID: e.ChunkID,
		Filename: e.Filename, Snippet: e.Snippet, Stance: e.Stance, Score: e.Score,
		Ord: int(e.Ord), CreatedAt: e.CreatedAt,
		Relation: e.Relation, PageStart: int(e.PageStart), PageEnd: int(e.PageEnd),
		SectionHeading: e.SectionHeading, Origin: e.Origin,
	}
}

func revisionFromRow(rev sqlcgen.HypothesisRevision) *domain.Revision {
	return &domain.Revision{
		ID: rev.ID, HypothesisID: rev.HypothesisID, RevisionNo: int(rev.RevisionNo), EditorID: rev.EditorID,
		Action: rev.Action, Summary: rev.Summary, Patch: rev.Patch, CreatedAt: rev.CreatedAt,
	}
}

func insertHypothesisParams(h *domain.Hypothesis) sqlcgen.InsertHypothesisParams {
	return sqlcgen.InsertHypothesisParams{
		ID: h.ID, OwnerID: h.OwnerID, RunID: h.RunID, Title: h.Title, Statement: h.Statement,
		Rationale: h.Rationale, Method: h.Method, Status: h.Status, KpiID: h.KPIID,
		PrimaryClusterID: h.PrimaryClusterID, Trl: intToI16Ptr(h.TRL), NoveltyScore: h.NoveltyScore,
		RiskScore: h.RiskScore, ValueScore: h.ValueScore, ConfidenceScore: h.ConfidenceScore,
		CompositeScore: h.CompositeScore, Measurable: h.Measurable, Organization: h.Organization,
		FunctionArea: h.FunctionArea, SourceType: h.SourceType, Location: h.Location, Tags: orEmpty(h.Tags),
		Assessment: jsonbOr(h.Assessment, "{}"), Detail: jsonbOr(h.Detail, "{}"),
		Generation: jsonbOr(h.Generation, "{}"), CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

func insertRevisionParams(rev *domain.Revision) sqlcgen.InsertRevisionParams {
	return sqlcgen.InsertRevisionParams{
		ID: rev.ID, HypothesisID: rev.HypothesisID, EditorID: rev.EditorID, Action: rev.Action,
		Summary: rev.Summary, Patch: jsonbOr(rev.Patch, "{}"), CreatedAt: rev.CreatedAt,
	}
}

func (r *HypothesisRepo) Create(ctx context.Context, h *domain.Hypothesis, initial *domain.Revision) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create hypothesis: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	operation := "create-hypothesis/" + contracts.HypothesisLeaseFrom(ctx).Step
	previous, err := lockHypothesisEffect(ctx, tx, h.OwnerID, operation)
	if err != nil {
		return err
	}
	if previous != "" {
		stored, e := readStoredHypothesis(ctx, qtx, previous)
		if e != nil {
			return e
		}
		*h = *stored
		return nil
	}

	if ierr := qtx.InsertHypothesis(ctx, insertHypothesisParams(h)); ierr != nil {
		return fmt.Errorf("insert hypothesis: %w", ierr)
	}
	for _, e := range h.Evidence {
		if ierr := qtx.InsertEvidence(ctx, sqlcgen.InsertEvidenceParams{
			ID: e.ID, HypothesisID: e.HypothesisID, DocumentID: e.DocumentID, ChunkID: e.ChunkID,
			Filename: e.Filename, Snippet: e.Snippet, Stance: e.Stance, Score: e.Score,
			Ord: int32(e.Ord), CreatedAt: e.CreatedAt, Relation: e.Relation,
			PageStart: int32(e.PageStart), PageEnd: int32(e.PageEnd), SectionHeading: e.SectionHeading,
			Origin: e.Origin,
		}); ierr != nil {
			return fmt.Errorf("insert evidence: %w", ierr)
		}
	}
	if initial != nil {
		no, rerr := qtx.InsertRevision(ctx, insertRevisionParams(initial))
		if rerr != nil {
			return fmt.Errorf("insert revision: %w", rerr)
		}
		initial.RevisionNo = int(no)
	}
	if err = saveHypothesisEffect(ctx, tx, operation, h.ID); err != nil {
		return err
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return fmt.Errorf("commit create hypothesis: %w", cerr)
	}
	h.Version = 1
	return nil
}

func (r *HypothesisRepo) Get(ctx context.Context, id string) (*domain.Hypothesis, error) {
	row, err := r.q.GetHypothesis(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select hypothesis: %w", err)
	}
	h := hypothesisFromRow(row)
	ev, eerr := r.ListEvidence(ctx, id)
	if eerr != nil {
		return nil, eerr
	}
	h.Evidence = ev
	return h, nil
}

func (r *HypothesisRepo) List(ctx context.Context, f domain.HypothesisFilter) ([]*domain.Hypothesis, error) {
	rows, err := r.q.ListHypotheses(ctx, hypothesisListParams(f))
	if err != nil {
		return nil, fmt.Errorf("list hypotheses: %w", err)
	}
	out := make([]*domain.Hypothesis, 0, len(rows))
	for i := range rows {
		out = append(out, hypothesisFromRow(sqlcgen.Hypothesis(rows[i])))
	}
	return out, nil
}

func (r *HypothesisRepo) Update(ctx context.Context, h *domain.Hypothesis, rev *domain.Revision) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update hypothesis: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	operation := "update-hypothesis/" + h.ID + "/" + contracts.HypothesisLeaseFrom(ctx).Step
	if rev != nil {
		operation += "/" + rev.Action
	}
	previous, err := lockHypothesisEffect(ctx, tx, h.OwnerID, operation)
	if err != nil {
		return err
	}
	if previous != "" {
		stored, e := readStoredHypothesis(ctx, qtx, previous)
		if e != nil {
			return e
		}
		*h = *stored
		return nil
	}

	n, uerr := qtx.UpdateHypothesis(ctx, sqlcgen.UpdateHypothesisParams{
		ExpectedVersion: h.Version, OwnerID: h.OwnerID,
		ID: h.ID, Title: h.Title, Statement: h.Statement, Rationale: h.Rationale, Method: h.Method,
		Status: h.Status, KpiID: h.KPIID, PrimaryClusterID: h.PrimaryClusterID, Trl: intToI16Ptr(h.TRL),
		NoveltyScore: h.NoveltyScore, RiskScore: h.RiskScore, ValueScore: h.ValueScore,
		ConfidenceScore: h.ConfidenceScore, CompositeScore: h.CompositeScore, Measurable: h.Measurable,
		Organization: h.Organization, FunctionArea: h.FunctionArea, SourceType: h.SourceType,
		Location: h.Location, Tags: orEmpty(h.Tags), Assessment: jsonbOr(h.Assessment, "{}"),
		Detail: jsonbOr(h.Detail, "{}"), Generation: jsonbOr(h.Generation, "{}"),
	})
	if uerr != nil {
		return fmt.Errorf("update hypothesis: %w", uerr)
	}
	if n == 0 {
		return domain.ErrConflict
	}
	var evidence []*domain.Evidence
	if rev != nil && rev.Action == "verified" {
		evidence, err = replaceVerifiedEvidence(ctx, tx, qtx, h)
	} else {
		evidence, err = insertNewEvidence(ctx, qtx, h)
	}
	if err != nil {
		return err
	}
	if rev != nil {
		no, rerr := qtx.InsertRevision(ctx, insertRevisionParams(rev))
		if rerr != nil {
			return fmt.Errorf("insert revision: %w", rerr)
		}
		rev.RevisionNo = int(no)
	}
	if err = saveHypothesisEffect(ctx, tx, operation, h.ID); err != nil {
		return err
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return fmt.Errorf("commit update hypothesis: %w", cerr)
	}
	h.Evidence = evidence
	h.Version++
	return nil
}

func (r *HypothesisRepo) ListEvidence(ctx context.Context, hypothesisID string) ([]*domain.Evidence, error) {
	rows, err := r.q.ListEvidenceByHypothesis(ctx, hypothesisID)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	out := make([]*domain.Evidence, 0, len(rows))
	for i := range rows {
		out = append(out, evidenceFromRow(rows[i]))
	}
	return out, nil
}

func (r *HypothesisRepo) AddRevision(ctx context.Context, rev *domain.Revision) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Все писатели держат блокировку одной гипотезы до назначения номера ревизии.
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM hypotheses WHERE id=$1 FOR UPDATE`, rev.HypothesisID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("lock hypothesis revision: %w", err)
	}
	no, err := r.q.WithTx(tx).InsertRevision(ctx, insertRevisionParams(rev))
	if err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revision: %w", err)
	}
	rev.RevisionNo = int(no)
	return nil
}

func (r *HypothesisRepo) ListRevisions(ctx context.Context, hypothesisID string) ([]*domain.Revision, error) {
	rows, err := r.q.ListRevisionsByHypothesis(ctx, hypothesisID)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	out := make([]*domain.Revision, 0, len(rows))
	for i := range rows {
		out = append(out, revisionFromRow(rows[i]))
	}
	return out, nil
}

func readStoredHypothesis(ctx context.Context, q *sqlcgen.Queries, id string) (*domain.Hypothesis, error) {
	row, err := q.GetHypothesis(ctx, id)
	if err != nil {
		return nil, err
	}
	out := hypothesisFromRow(row)
	evidence, err := q.ListEvidenceByHypothesis(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, e := range evidence {
		out.Evidence = append(out.Evidence, evidenceFromRow(e))
	}
	return out, nil
}

func hypothesisListParams(f domain.HypothesisFilter) sqlcgen.ListHypothesesParams {
	return sqlcgen.ListHypothesesParams{
		BeforeID: f.BeforeID,
		Search:   f.Search, Queue: f.Queue, Compact: f.Compact, OwnerID: f.OwnerID, Status: f.Status, KpiID: f.KPIID,
		ClusterID:    f.ClusterID,
		FunctionArea: f.FunctionArea, SourceType: f.SourceType, Organization: f.Organization,
		MinTrl: int32(f.MinTRL), MaxTrl: int32(f.MaxTRL), Tags: orEmpty(f.Tags),
		DocumentIds: orEmpty(f.DocumentIDs),
		OrderBy:     f.OrderBy, Lim: int32(f.Limit), Off: int32(f.Offset),
	}
}

func insertNewEvidence(ctx context.Context, qtx *sqlcgen.Queries, h *domain.Hypothesis) ([]*domain.Evidence, error) {
	out := make([]*domain.Evidence, len(h.Evidence))
	for i, original := range h.Evidence {
		e := *original
		out[i] = original
		if e.ID != "" {
			continue
		}
		e.ID = uuid.NewV4().String()
		e.HypothesisID = h.ID
		if e.Stance == "" {
			e.Stance = "context"
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now().UTC()
		}
		if ierr := qtx.InsertEvidence(ctx, sqlcgen.InsertEvidenceParams{
			ID: e.ID, HypothesisID: e.HypothesisID, DocumentID: e.DocumentID, ChunkID: e.ChunkID,
			Filename: e.Filename, Snippet: e.Snippet, Stance: e.Stance, Score: e.Score,
			Ord: int32(e.Ord), CreatedAt: e.CreatedAt, Relation: e.Relation,
			PageStart: int32(e.PageStart), PageEnd: int32(e.PageEnd), SectionHeading: e.SectionHeading,
			Origin: e.Origin,
		}); ierr != nil {
			return nil, fmt.Errorf("insert evidence on update: %w", ierr)
		}
		out[i] = &e
	}
	return out, nil
}
