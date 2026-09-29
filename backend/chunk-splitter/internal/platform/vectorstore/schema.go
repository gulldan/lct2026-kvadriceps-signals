package vectorstore

import "fmt"

const (
	cosineDistance = "Cosine"
	keywordPayload = "keyword"
)

type responseError struct {
	status  int
	message string
}

func (e *responseError) Error() string { return e.message }

type collectionSchema struct {
	Result struct {
		Config struct {
			Params struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			} `json:"params"`
		} `json:"config"`
		PayloadSchema map[string]struct {
			DataType string `json:"data_type"`
		} `json:"payload_schema"`
	} `json:"result"`
}

func (s collectionSchema) validate(dim int) error {
	vectors := s.Result.Config.Params.Vectors
	if vectors.Size != dim || vectors.Distance != cosineDistance {
		return fmt.Errorf("incompatible vector collection: size=%d distance=%s, need size=%d Cosine; reindex a new collection",
			vectors.Size, vectors.Distance, dim)
	}
	for field, kind := range map[string]string{
		"owner_id":    keywordPayload,
		"document_id": keywordPayload,
		"node_type":   keywordPayload,
		"chunk_index": "integer",
	} {
		if existing, found := s.Result.PayloadSchema[field]; found && existing.DataType != kind {
			return fmt.Errorf("incompatible payload index %s: %s, require %s", field, existing.DataType, kind)
		}
	}
	return nil
}
