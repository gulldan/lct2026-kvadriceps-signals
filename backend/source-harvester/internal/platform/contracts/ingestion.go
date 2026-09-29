package contracts

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"uuid"
)

// ErrDurability требует сохранить исходную доставку до восстановления хранилища.
var ErrDurability = errors.New("mandatory ingestion persistence failed")

type documentGenerationKey struct{}

func WithDocumentGeneration(ctx context.Context, generation int64) context.Context {
	return context.WithValue(ctx, documentGenerationKey{}, max(1, generation))
}

// DocumentGeneration допускает старые сообщения только в первом поколении.
func DocumentGeneration(ctx context.Context) int64 {
	generation, _ := ctx.Value(documentGenerationKey{}).(int64)
	return max(1, generation)
}

func StableID(namespace string, parts ...string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + strings.Join(parts, "\x00")))
	id := uuid.UUID(sum[:16])
	id[6] = id[6]&0x0f | 0x80
	id[8] = id[8]&0x3f | 0x80
	return id.String()
}
