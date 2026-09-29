package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
)

func ParsedMetadata(text, revision string, metadata map[string]string) map[string]string {
	out := maps.Clone(metadata)
	if out == nil {
		out = map[string]string{}
	}
	sum := sha256.Sum256([]byte(text))
	out["parsed_text_sha256"] = hex.EncodeToString(sum[:])
	out["parser_revision"] = revision
	return out
}

// ParsedObjectKey адресует текст по его хэшу, чтобы повтор или улучшенное извлечение не затёрли текст,
// на который ссылается уже поставленное в очередь событие.
func ParsedObjectKey(documentID, text string) string {
	sum := sha256.Sum256([]byte(text))
	return "parsed/v2/" + documentID + "/" + hex.EncodeToString(sum[:]) + ".txt"
}
