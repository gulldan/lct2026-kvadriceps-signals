package vectorstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
)

const (
	qAccessScope = "access_scope"
	qLanguage    = "language"
	qRole        = "role"
	qStage       = "stage"
)

// TechnologyNamespace включает модель эмбеддингов в имя: векторы разных моделей несравнимы,
// поэтому смена модели даёт новое пространство, а не дозапись.
func TechnologyNamespace(model, revision string) string {
	// Экранирование сохраняет значимость пунктуации и регистра и исключает неоднозначность.
	sum := sha256.Sum256([]byte(strconv.Quote(model) + "@" + strconv.Quote(revision)))
	return "technologies-" + hex.EncodeToString(sum[:])
}

func (q *Qdrant) EnsureTechnologyCollection(ctx context.Context, dim int) error {
	if err := q.do(ctx, http.MethodGet, "/collections/"+q.collection, nil, nil); err != nil {
		body := map[string]any{"vectors": map[string]any{"size": dim, "distance": cosineDistance}}
		if cerr := q.do(ctx, http.MethodPut, "/collections/"+q.collection, body, nil); cerr != nil {
			return cerr
		}
	}
	for _, field := range []string{qAccessScope, qOwnerID, qLanguage, qRole, qStage, "technology_id"} {
		if err := q.ensurePayloadIndex(ctx, field, keywordPayload); err != nil {
			return err
		}
	}
	return nil
}

// TechnologyPoint несёт модель и в payload, чтобы точку, попавшую не в ту коллекцию, можно было отличить.
type TechnologyPoint struct {
	ID                string    `json:"id"`
	Vector            []float32 `json:"vector"`
	TechnologyID      string    `json:"technology_id"`
	OwnerID           string    `json:"owner_id"`
	AccessScope       string    `json:"access_scope"`
	Language          string    `json:"language"`
	Role              string    `json:"role"`
	Stage             string    `json:"stage"`
	EmbeddingModel    string    `json:"embedding_model"`
	EmbeddingRevision string    `json:"embedding_revision"`
}

func (q *Qdrant) UpsertTechnologies(ctx context.Context, model, revision string, points []TechnologyPoint) error {
	if len(points) == 0 {
		return nil
	}
	want := TechnologyNamespace(model, revision)
	if q.collection != want {
		return fmt.Errorf("technology namespace %q does not hold vectors of %s@%s", q.collection, model, revision)
	}
	written := make([]Point, 0, len(points))
	for _, point := range points {
		if point.EmbeddingModel != model || point.EmbeddingRevision != revision {
			return fmt.Errorf("technology %s carries vectors of another model", point.TechnologyID)
		}
		written = append(written, Point{ID: point.ID, Vector: point.Vector, Payload: map[string]any{
			"technology_id": point.TechnologyID, qOwnerID: point.OwnerID, qAccessScope: point.AccessScope,
			qLanguage: point.Language, qRole: point.Role, qStage: point.Stage,
			"embedding_model": point.EmbeddingModel, "embedding_revision": point.EmbeddingRevision,
		}})
	}
	return q.Upsert(ctx, written)
}

type TechnologyFilter struct {
	Reader       string
	SharedCorpus bool
	Languages    []string
	Roles        []string
	Stages       []string
}

// SearchFilter строит условие доступа здесь, а не у вызывающего: фильтр, который можно забыть,
// не является правилом доступа.
func (f TechnologyFilter) SearchFilter() SearchFilter {
	scopes := []string{"public"}
	if f.SharedCorpus {
		scopes = append(scopes, "shared")
	}
	// ИЛИ по двум полям: И скрыло бы чужие публичные технологии.
	filter := SearchFilter{AnyOf: map[string][]string{}, EitherOf: map[string][]string{qAccessScope: scopes}}
	if f.Reader != "" {
		filter.EitherOf[qOwnerID] = []string{f.Reader}
	}
	for field, values := range map[string][]string{
		qLanguage: f.Languages, qRole: f.Roles, qStage: f.Stages,
	} {
		if len(values) > 0 {
			filter.AnyOf[field] = values
		}
	}
	return filter
}
