package postgres_test

import (
	"encoding/json/v2"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

const (
	extractorVersion = "extractor-test-v1"
	schemaVersion    = "technology-extraction-v1"
)

func seedExtraction(t *testing.T, q *sqlcgen.Queries, revision, status string, sections int32) {
	t.Helper()
	if err := q.RecordTechnologyExtraction(t.Context(), sqlcgen.RecordTechnologyExtractionParams{
		SourceRevisionID:  revision,
		ExtractorVersion:  extractorVersion,
		SchemaVersion:     schemaVersion,
		Status:            status,
		ReasonCode:        "",
		ProcessedSections: sections,
		TotalSections:     sections,
	}); err != nil {
		t.Fatalf("record extraction: %v", err)
	}
}

func seedTechnology(t *testing.T, db *postgres.DB, revision, name string) (string, string) {
	t.Helper()
	ctx := t.Context()
	tech, mention, evidence := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlcgen.New(db.Pool).WithTx(tx)
	if err = q.CreateTechnology(ctx, sqlcgen.CreateTechnologyParams{
		ID: tech, CanonicalName: name, NameKind: "descriptive", Description: "Описание " + name,
		ValidationStatus: "needs_review", MethodVersion: "signalgraph-method-v7",
	}); err != nil {
		t.Fatalf("create technology: %v", err)
	}
	if err = q.CreateTechnologyMention(ctx, sqlcgen.CreateTechnologyMentionParams{
		ID: mention, TechnologyID: tech, SourceRevisionID: revision, ExtractorVersion: extractorVersion,
		SchemaVersion: schemaVersion, Name: name, Description: "Описание " + name,
		Role: "proposed_contribution", Problem: "Выбор способа поиска", Mechanism: "Маршрутизация по запросу",
		Inputs: []string{"Запрос"}, Outputs: []string{"Выбранный способ"}, ExtractionStatus: "complete",
	}); err != nil {
		t.Fatalf("create mention: %v", err)
	}
	if err = q.AddTechnologyEvidence(ctx, sqlcgen.AddTechnologyEvidenceParams{
		ID: evidence, MentionID: mention, SourceRevisionID: revision, Section: "method",
		Page: ptrI32(4), StartChar: 1020, EndChar: 1130, Quote: "Точная подстрока сохранённого источника",
		ElementPath: "html/body/article/p[3]", SnapshotSha256: strings.Repeat("a", 64),
	}); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	if err = q.LinkTechnologyFieldEvidence(ctx, sqlcgen.LinkTechnologyFieldEvidenceParams{
		MentionID: mention, Field: "mechanism", EvidenceID: evidence,
	}); err != nil {
		t.Fatalf("link field evidence: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatalf("commit technology: %v", err)
	}
	return tech, mention
}

func ptrI32(v int32) *int32 { return &v }

func TestTechnologyModel(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	q := sqlcgen.New(db.Pool)
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, q, revision, "complete", 12)

	techs := make([]string, 0, 3)
	mentions := make([]string, 0, 3)
	for _, name := range []string{"Адаптивная маршрутизация", "Разреженное внимание", "Каскадный ранжировщик"} {
		tech, mention := seedTechnology(t, db, revision, name)
		techs = append(techs, tech)
		mentions = append(mentions, mention)
	}

	stored, err := q.ListRevisionMentions(ctx, sqlcgen.ListRevisionMentionsParams{
		SourceRevisionID: revision, ExtractorVersion: extractorVersion,
	})
	if err != nil || len(stored) != 3 {
		t.Fatalf("expected three mentions, got %d: %v", len(stored), err)
	}
	evidence, err := q.ListMentionEvidence(ctx, mentions)
	if err != nil || len(evidence) != 3 {
		t.Fatalf("expected three quotes, got %d: %v", len(evidence), err)
	}
	fields, err := q.ListMentionFieldEvidence(ctx, mentions)
	if err != nil || len(fields) != 3 {
		t.Fatalf("expected three field links, got %d: %v", len(fields), err)
	}
	if stored[0].Inputs[0] != "Запрос" || stored[0].Role != "proposed_contribution" {
		t.Fatalf("mention not round-tripped: %+v", stored[0])
	}
	// Каждая часть локатора должна пережить хранение, иначе адрес цитаты потеряется.
	for _, row := range evidence {
		if row.Section != "method" || row.ElementPath != "html/body/article/p[3]" ||
			row.SnapshotSha256 != strings.Repeat("a", 64) || row.StartChar >= row.EndChar {
			t.Fatalf("the evidence locator was not stored in full: %+v", row)
		}
	}

	foreign := seedDoc(t, db, owner)
	t.Run("unsupported language is its own answer", func(t *testing.T) {
		other := foreign
		if err := q.RecordTechnologyExtraction(ctx, sqlcgen.RecordTechnologyExtractionParams{
			SourceRevisionID: other, ExtractorVersion: extractorVersion, SchemaVersion: schemaVersion,
			Status: "unsupported_language", ReasonCode: "de", TotalSections: 4,
		}); err != nil {
			t.Fatalf("a material outside the supported languages could not be recorded: %v", err)
		}
		stored, err := q.GetTechnologyExtraction(ctx, sqlcgen.GetTechnologyExtractionParams{
			SourceRevisionID: other, ExtractorVersion: extractorVersion, SchemaVersion: schemaVersion,
		})
		if err != nil || stored.Status != "unsupported_language" || stored.ProcessedSections != 0 {
			t.Fatalf("the answer was not stored as its own: %+v %v", stored, err)
		}
		// У непрочитанного материала нет обработанных разделов, иначе он считался бы проверенным.
		if err = q.RecordTechnologyExtraction(ctx, sqlcgen.RecordTechnologyExtractionParams{
			SourceRevisionID: other, ExtractorVersion: extractorVersion, SchemaVersion: schemaVersion,
			Status: "unsupported_language", ProcessedSections: 4, TotalSections: 4,
		}); err == nil {
			t.Fatal("an unread material was recorded as fully processed")
		}
	})

	t.Run("a locator carries a real snapshot hash", func(t *testing.T) {
		err := q.AddTechnologyEvidence(ctx, sqlcgen.AddTechnologyEvidenceParams{
			ID: uuid.NewV4().String(), MentionID: mentions[0], SourceRevisionID: revision,
			Section: "claims", StartChar: 1, EndChar: 2, Quote: "x",
			SnapshotSha256: "not-a-hash",
		})
		if err == nil {
			t.Fatal("a locator naming a snapshot that cannot exist was stored")
		}
	})

	t.Run("mention needs a revision", func(t *testing.T) {
		_, err := db.Pool.Exec(t.Context(), `INSERT INTO technology_mentions
(id,technology_id,source_revision_id,extractor_version,schema_version,name,role,extraction_status)
VALUES($1,$2,NULL,$3,$4,'x','uncertain','complete')`,
			uuid.NewV4().String(), techs[0], extractorVersion, schemaVersion)
		if err == nil {
			t.Fatal("mention without a source revision must be rejected")
		}
	})

	t.Run("evidence stays in its revision", func(t *testing.T) {
		other := seedDoc(t, db, owner)
		seedExtraction(t, q, other, "complete", 1)
		err := q.AddTechnologyEvidence(t.Context(), sqlcgen.AddTechnologyEvidenceParams{
			ID: uuid.NewV4().String(), MentionID: mentions[0], SourceRevisionID: other, Section: "1",
			Page: ptrI32(1), StartChar: 0, EndChar: 10, Quote: "чужая ревизия",
		})
		if err == nil {
			t.Fatal("evidence of a foreign revision must be rejected")
		}
	})

	t.Run("same_as is one ordered fact", func(t *testing.T) {
		relate := func(from, to string) error {
			return q.AddTechnologyRelation(t.Context(), sqlcgen.AddTechnologyRelationParams{
				TechnologyID: from, RelatedTechnologyID: to, Relation: "same_as",
				Basis: "совпадает механизм и задача", DecisionVersion: "identity-v1",
			})
		}
		if relate(techs[0], techs[0]) == nil {
			t.Fatal("same_as onto itself must be rejected")
		}
		low, high := min(techs[0], techs[1]), max(techs[0], techs[1])
		if err := relate(low, high); err != nil {
			t.Fatalf("relate: %v", err)
		}
		if relate(low, high) == nil {
			t.Fatal("duplicate pair must be rejected")
		}
		if relate(high, low) == nil {
			t.Fatal("mirrored pair must be rejected")
		}
	})

	t.Run("technology needs a mention", func(t *testing.T) {
		tx, err := db.Pool.Begin(t.Context())
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(t.Context()) }()
		if err = sqlcgen.New(db.Pool).WithTx(tx).CreateTechnology(t.Context(), sqlcgen.CreateTechnologyParams{
			ID: uuid.NewV4().String(), CanonicalName: "Без упоминаний", NameKind: "descriptive",
			Description: "", ValidationStatus: "proposed", MethodVersion: "signalgraph-method-v7",
		}); err != nil {
			t.Fatalf("create technology: %v", err)
		}
		if tx.Commit(t.Context()) == nil {
			t.Fatal("technology without a mention must be rejected")
		}
	})

	t.Run("empty technology set is a record", func(t *testing.T) {
		empty := seedDoc(t, db, owner)
		seedExtraction(t, q, empty, "no_technology", 7)
		got, err := q.GetTechnologyExtraction(t.Context(), sqlcgen.GetTechnologyExtractionParams{
			SourceRevisionID: empty, ExtractorVersion: extractorVersion, SchemaVersion: schemaVersion,
		})
		if err != nil || got.Status != "no_technology" || got.ProcessedSections != 7 {
			t.Fatalf("examined revision must keep its record: %+v %v", got, err)
		}
		mentions, err := q.ListRevisionMentions(t.Context(), sqlcgen.ListRevisionMentionsParams{
			SourceRevisionID: empty, ExtractorVersion: "",
		})
		if err != nil || len(mentions) != 0 {
			t.Fatalf("expected no mentions, got %d: %v", len(mentions), err)
		}
	})
}

// Down-блок миграции 27 должен отказать, а не каскадно удалить документы.
func TestTechnologyMigrationRollbackKeepsDocuments(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	raw, err := os.ReadFile("migrations/00027_technology_model.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	_, down, found := strings.Cut(string(raw), "-- +goose Down")
	if !found {
		t.Fatal("migration has no Down section")
	}
	down = strings.ReplaceAll(down, "-- +goose StatementBegin", "")
	down = strings.ReplaceAll(down, "-- +goose StatementEnd", "")

	revision := seedDoc(t, db, uuid.NewV4().String())
	err = pgx.BeginFunc(ctx, db.Pool, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, down)
		return execErr
	})
	if err == nil {
		t.Fatal("rollback of the technology model must be refused")
	}
	var kept int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM documents WHERE id=$1`, revision).Scan(&kept); err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if kept != 1 {
		t.Fatal("refused rollback must leave document data untouched")
	}
}

// seedPublicDoc даёт проекции что оставить, когда закрытое скрыто.
func seedPublicDoc(t *testing.T, db *postgres.DB, owner string) string {
	t.Helper()
	id := seedDoc(t, db, owner)
	native := uuid.NewV4().String()
	source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: native,
		CanonicalID: "paper:" + native, URL: "https://example.org/" + native,
		Publication: &sourcedoc.Date{Value: "2026-01-01", Precision: "day", Basis: "publisher record"},
		ObservedAt:  time.Now().UTC(), Revision: "v1", ContentHash: strings.Repeat("d", 64),
		HashAlgorithm: "sha256", AccessScope: "public"}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(t.Context(),
		`UPDATE documents SET source_metadata=$2::jsonb WHERE id=$1`, id, string(raw)); err != nil {
		t.Fatalf("publish revision: %v", err)
	}
	return id
}

// attachMention растягивает одну технологию на два уровня доступа.
func attachMention(t *testing.T, db *postgres.DB, technology, revision, name string) {
	t.Helper()
	q := sqlcgen.New(db.Pool)
	if err := q.CreateTechnologyMention(t.Context(), sqlcgen.CreateTechnologyMentionParams{
		ID: uuid.NewV4().String(), TechnologyID: technology, SourceRevisionID: revision,
		ExtractorVersion: extractorVersion, SchemaVersion: schemaVersion, Name: name,
		Description: "Описание " + name, Role: "proposed_contribution", Problem: "Выбор способа",
		Mechanism: "Мембрана", Inputs: []string{"Вода"}, Outputs: []string{"Водород"},
		ExtractionStatus: "complete",
	}); err != nil {
		t.Fatalf("attach mention: %v", err)
	}
}
