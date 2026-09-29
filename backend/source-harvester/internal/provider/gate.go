package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

// Gate резервирует общие для провайдера слоты запросов и хранит паузы из ответов;
// сбой резерва или записи останавливает страницу до сдвига курсора.
type Gate interface {
	Before(context.Context) error
	After(context.Context, time.Time) error
}

func SetGate(adapter Adapter, gate Gate) error {
	a, ok := adapter.(interface{ SetRequestGate(Gate) })
	if !ok {
		return errors.New("adapter does not support a durable request gate")
	}
	a.SetRequestGate(gate)
	return nil
}

func (a *Crossref) SetRequestGate(g Gate) {
	a.fetcher.gate = g
	if a.papers != nil {
		a.papers.fetcher.gate = g
	}
}

// SetRequestGate пропускает через резерв в базе каждый запрос arXiv, включая повторы.
func (a *ArXiv) SetRequestGate(g Gate) { a.fetcher.gate = g }

func (a *GitHub) SetRequestGate(g Gate) { a.fetcher.gate = g }

// PDF идёт через тот же ограничитель arXiv, что и Atom; адрес строится из проверенного идентификатора версии,
// а не из ссылки ленты или переадресации. Сначала файл ищется в массовой выгрузке: там нет предела частоты,
// а на arxiv.org каждый PDF стоил три секунды общего предела и ночь на десять тысяч работ.
func (a *ArXiv) PDF(ctx context.Context, source sourcedoc.Source) ([]byte, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	if source.Provider != providerArXiv {
		return nil, errors.New("PDF requires an arXiv source")
	}
	if raw, err := a.mirrorPDF(ctx, source.Revision); raw != nil || errors.Is(err, ErrResponseTooLarge) {
		return raw, err
	}
	raw, _, err := a.fetcher.get(ctx, "https://arxiv.org/pdf/"+source.Revision)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return nil, errors.New("arXiv PDF response has no PDF signature")
	}
	return raw, nil
}

// mirrorPDF — файл из выгрузки или nil: файла нет, сеть дважды зависла или идентификатор старого вида
// (archive/NNNNNNN) — тогда решает arxiv.org. Зависание — свойство соединения, а не файла: вторая попытка
// на новом соединении обычно проходит за секунду. Слишком большой файл будет таким же и на arxiv.org.
func (a *ArXiv) mirrorPDF(ctx context.Context, revision string) ([]byte, error) {
	month, _, ok := strings.Cut(revision, ".")
	if a.mirror == "" || a.mirrorHTTP == nil || !ok || strings.Contains(revision, "/") {
		return nil, nil
	}
	for range 2 {
		raw, found, err := a.mirrorOnce(ctx, a.mirror+"/"+month+"/"+revision+".pdf")
		if raw != nil || err != nil || !found {
			return raw, err
		}
	}
	return nil, nil
}

// mirrorOnce — одна попытка: found=false — файла в выгрузке нет, повторять незачем; nil и found — сбой соединения.
func (a *ArXiv) mirrorOnce(ctx context.Context, url string) (raw []byte, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, false, nil
	}
	req.Header.Set("User-Agent", "rag-test-source-harvester/1.0")
	resp, err := a.mirrorHTTP.Do(req)
	if err != nil {
		return nil, true, nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= http.StatusInternalServerError, nil
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	switch {
	case err != nil:
		return nil, true, nil
	case len(raw) > maxResponseBytes:
		return nil, true, ErrResponseTooLarge
	case !bytes.HasPrefix(raw, []byte("%PDF-")):
		return nil, false, nil
	}
	return raw, true, nil
}
