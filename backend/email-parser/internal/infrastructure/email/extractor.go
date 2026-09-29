package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/example/email-parser/internal/domain"
)

// Магическое начало OLE2-контейнера Outlook .msg; текстовый .eml так начинаться не может.
const oleSignature = "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1"

type Extractor struct {
	tikaURL string
	client  *http.Client
}

// NewExtractor с пустым tikaURL роняет .msg намеренно: лучше статус failed, чем проиндексированная заглушка.
func NewExtractor(tikaURL string) *Extractor {
	return &Extractor{
		tikaURL: strings.TrimRight(tikaURL, "/"),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *Extractor) Extract(ctx context.Context, data []byte) (string, error) {
	doc, err := e.ExtractWithMeta(ctx, data)
	return doc.Text, err
}

func (e *Extractor) ExtractWithMeta(ctx context.Context, data []byte) (domain.ExtractedEmail, error) {
	if bytes.HasPrefix(data, []byte(oleSignature)) {
		text, err := e.extractMSG(ctx, data)
		return domain.ExtractedEmail{Text: text}, err
	}
	return e.extractEML(data)
}

func (e *Extractor) extractEML(data []byte) (domain.ExtractedEmail, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return domain.ExtractedEmail{}, fmt.Errorf("read message: %w", err)
	}

	dec := new(mime.WordDecoder)
	var header strings.Builder
	var author string
	for _, key := range []string{"Subject", "From", "To", "Date"} {
		raw := msg.Header.Get(key)
		if raw == "" {
			continue
		}
		value := raw
		if decoded, derr := dec.DecodeHeader(raw); derr == nil {
			value = decoded
		}
		if key == "From" {
			author = value
		}
		fmt.Fprintf(&header, "%s: %s\n", key, value)
	}

	body, attachments, err := e.readBody(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
	if err != nil {
		return domain.ExtractedEmail{}, fmt.Errorf("read body: %w", err)
	}

	var out strings.Builder
	out.WriteString(strings.TrimRight(header.String(), "\n"))
	out.WriteString("\n\n")
	out.WriteString(strings.TrimSpace(body))
	if len(attachments) > 0 {
		out.WriteString("\n\nAttachments: ")
		out.WriteString(strings.Join(attachments, ", "))
	}
	return domain.ExtractedEmail{
		Text:        strings.TrimSpace(out.String()),
		Author:      author,
		PublishedAt: publishedAt(msg.Header),
	}, nil
}

func publishedAt(h mail.Header) string {
	if d, err := h.Date(); err == nil {
		return d.Format(time.RFC3339)
	}
	return h.Get("Date")
}

func (e *Extractor) readBody(contentType, encoding string, body io.Reader) (text string, attachments []string, err error) {
	mediaType, params, perr := mime.ParseMediaType(contentType)
	if perr != nil {
		decoded, derr := decodePart(body, encoding)
		if derr != nil {
			return "", nil, fmt.Errorf("read plain body: %w", derr)
		}
		return string(decoded), nil, nil
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil, errors.New("multipart message without boundary")
		}
		return e.readMultipart(body, boundary)
	}

	// Однокомпонентное письмо (в том числе text/html) берётся целиком, чтобы документ не оказался пустым.
	decoded, derr := decodePart(body, encoding)
	if derr != nil {
		return "", nil, fmt.Errorf("read body: %w", derr)
	}
	return string(decoded), nil, nil
}

func (e *Extractor) readMultipart(body io.Reader, boundary string) (string, []string, error) {
	mr := multipart.NewReader(body, boundary)
	var texts []string
	var attachments []string

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("next part: %w", err)
		}
		if herr := e.handlePart(part, &texts, &attachments); herr != nil {
			return "", nil, herr
		}
	}

	return strings.Join(texts, "\n"), attachments, nil
}

func (e *Extractor) handlePart(part *multipart.Part, texts, attachments *[]string) error {
	partType, partParams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
	disposition, dispParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))

	if disposition == "attachment" {
		if name := attachmentName(dispParams, partParams); name != "" {
			*attachments = append(*attachments, name)
		}
		_ = part.Close()
		return nil
	}

	if strings.HasPrefix(partType, "multipart/") {
		nestedText, nestedAtt, nerr := e.readMultipart(part, partParams["boundary"])
		if nerr != nil {
			return nerr
		}
		if nestedText != "" {
			*texts = append(*texts, nestedText)
		}
		*attachments = append(*attachments, nestedAtt...)
		return nil
	}

	if partType == "text/plain" {
		decoded, derr := decodePart(part, part.Header.Get("Content-Transfer-Encoding"))
		if derr != nil {
			return fmt.Errorf("decode text part: %w", derr)
		}
		*texts = append(*texts, string(decoded))
	}
	return nil
}

func attachmentName(dispParams, partParams map[string]string) string {
	if name := dispParams["filename"]; name != "" {
		return name
	}
	return partParams["name"]
}

func decodePart(r io.Reader, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		raw, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read base64 part: %w", err)
		}
		// base64 в письмах разбит на строки: пробельные символы убираются до декодирования.
		clean := strings.Map(func(c rune) rune {
			if c == '\r' || c == '\n' || c == ' ' || c == '\t' {
				return -1
			}
			return c
		}, string(raw))
		decoded, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			return nil, fmt.Errorf("decode base64: %w", err)
		}
		return decoded, nil
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(r))
		if err != nil {
			return nil, fmt.Errorf("decode quoted-printable: %w", err)
		}
		return decoded, nil
	default:
		raw, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read part: %w", err)
		}
		return raw, nil
	}
}

func (e *Extractor) extractMSG(ctx context.Context, data []byte) (string, error) {
	if e.tikaURL == "" {
		return "", errors.New(".msg parsing requires Tika; set TIKA_URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, e.tikaURL+"/tika/text", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build tika request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("Content-Type", "application/vnd.ms-outlook")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call tika: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read tika response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tika returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
