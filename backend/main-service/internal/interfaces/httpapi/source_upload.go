package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// readUploadSource: адреса источника — только провенанс, сервер по ним не ходит.
func readUploadSource(r *http.Request, file io.ReadSeeker, cmd *domain.UploadCommand, mimeType string) error {
	raw := r.Header.Get("X-Document-Source")
	if raw == "" {
		return nil
	}
	source, err := sourcedoc.Decode([]byte(raw))
	if err != nil {
		return fmt.Errorf("source manifest: %w", err)
	}
	if source.Provider != "manual" || !slices.Contains([]string{"paper", "patent", "analysis", "company_material"}, source.Kind) {
		return errors.New("manual import requires a paper, patent publication, report or company material")
	}
	if err = requirePatentIdentity(source); err != nil {
		return err
	}
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || (source.AccessScope != "owner" && !hasAnyRole(claims, "operator", "admin")) {
		return errors.New("only an operator can import public or shared provenance")
	}
	if cmd.Size > 8<<20 {
		return errors.New("source import is limited to 8 MiB")
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("rewind source upload: %w", seekErr)
	}
	h := sha256.New()
	if _, copyErr := io.Copy(h, file); copyErr != nil {
		return fmt.Errorf("hash source upload: %w", copyErr)
	}
	expected := hex.EncodeToString(h.Sum(nil))
	if source.HashAlgorithm == "blake3" {
		expected = cmd.Hash
	}
	if expected != source.ContentHash {
		return errors.New("source manifest hash differs from uploaded bytes")
	}
	metadata, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("encode source upload: %w", err)
	}
	cmd.SourceMetadata = string(metadata)
	if textErr := validateSourceUTF8(file, mimeType); textErr != nil {
		return textErr
	}
	if strings.HasPrefix(mimeType, "text/html") {
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			return fmt.Errorf("rewind HTML upload: %w", seekErr)
		}
		cmd.Text, err = htmlSourceText(file)
		return err
	}
	if !strings.HasPrefix(mimeType, "text/plain") && mimeType != "application/pdf" {
		return errors.New("source import supports PDF, UTF-8 text, Markdown and HTML")
	}
	return nil
}

// requirePatentIdentity требует номер патента у ручной публикации, чтобы её можно было сдедуплицировать
// с собранным корпусом и не посчитать вторым изобретением.
func requirePatentIdentity(source *sourcedoc.Source) error {
	if source.Kind != "patent" {
		if source.Patent != nil {
			return errors.New("patent identity belongs only to a patent import")
		}
		return nil
	}
	if source.Patent == nil {
		return errors.New("a patent import requires publication number, jurisdiction and kind code")
	}
	if source.Patent.KindCode == "" {
		return errors.New("a patent import requires its kind code")
	}
	return nil
}

func validateSourceUTF8(file io.ReadSeeker, mimeType string) error {
	if !strings.HasPrefix(mimeType, "text/") {
		return nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind source text: %w", err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil {
		return fmt.Errorf("read source text: %w", err)
	}
	if len(raw) > 8<<20 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("source text must be nonempty UTF-8 within 8 MiB")
	}
	return nil
}

func htmlSourceText(input io.Reader) (string, error) { return application.SourceHTMLText(input) }
