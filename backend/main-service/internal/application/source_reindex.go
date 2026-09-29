package application

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
	"lukechampine.com/blake3"
)

// ReindexSource ставит указанную владельцем ревизию в очередь; опубликованная
// версия остаётся доступна до полной публикации её замены.
func (s *IngestionService) ReindexSource(ctx context.Context, owner, id string, force bool) (string, error) {
	doc, err := s.docs.GetDocument(ctx, id)
	if err != nil {
		return "", err
	}
	if doc.GetOwnerId() != owner {
		return "", ErrNotFound
	}
	state, err := s.sourceAccess(ctx, doc, owner, false)
	if err != nil {
		return "", err
	}
	if state.Active != nil && !force {
		return "already_ready", nil
	}
	raw, err := s.reindexOriginal(ctx, doc)
	if err != nil {
		return "", err
	}
	inlineText := ""
	if strings.HasPrefix(doc.GetMimeType(), "text/") && len(raw) <= 3<<20 {
		if !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
			return "", errors.New("invalid UTF-8 source")
		}
		inlineText = string(raw)
		if strings.HasPrefix(doc.GetMimeType(), "text/html") {
			inlineText, err = SourceHTMLText(bytes.NewReader(raw))
			if err != nil {
				return "", err
			}
		}
	}
	if _, err = s.docs.RequeueDocument(ctx, doc.GetId(), max(1, doc.GetGeneration()), inlineText); err != nil {
		return "", err
	}
	return "queued", nil
}

func (s *IngestionService) reindexOriginal(ctx context.Context, doc *commonv1.Document) ([]byte, error) {
	stream, err := s.store.Get(ctx, doc.GetObjectKey())
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	raw, err := io.ReadAll(io.LimitReader(stream, (64<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read source original: %w", err)
	}
	blake := blake3.Sum256(raw)
	contentHash := hex.EncodeToString(blake[:])
	if len(raw) == 0 || len(raw) > 64<<20 || contentHash != doc.GetContentHash() {
		return nil, errors.New("document original differs from its immutable revision")
	}
	if doc.GetSourceMetadata() != "" {
		source, decodeErr := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
		if decodeErr != nil {
			return nil, decodeErr
		}
		sourceHash := indexdoc.Hash(raw)
		if source.HashAlgorithm == "blake3" {
			sourceHash = contentHash
		}
		if sourceHash != source.ContentHash {
			return nil, errors.New("source original differs from its immutable revision")
		}
	}
	return raw, nil
}
