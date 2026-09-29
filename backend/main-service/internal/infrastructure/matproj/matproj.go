package matproj

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/jsonx"
)

const summaryEndpoint = "https://api.materialsproject.org/materials/summary/"

// Новизна — лишь подсказка, поэтому медленный MP не должен тормозить генерацию.
const httpTimeout = 6 * time.Second

type MaterialSummary struct {
	Known      bool    `json:"known"`
	MaterialID string  `json:"material_id"`
	Formula    string  `json:"formula"`
	IsStable   bool    `json:"is_stable"`
	BandGap    float64 `json:"band_gap"`
	Density    float64 `json:"density"`
}

// Client без ключа — заглушка без сетевых вызовов: Summary отвечает «unknown», Known — false, без ошибок.
type Client struct {
	apiKey string
	http   *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		apiKey: strings.TrimSpace(apiKey),
		http:   &http.Client{Timeout: httpTimeout},
	}
}

type mpDoc struct {
	MaterialID  string   `json:"material_id"`
	FormulaPret string   `json:"formula_pretty"`
	IsStable    bool     `json:"is_stable"`
	BandGap     *float64 `json:"band_gap"`
	Density     *float64 `json:"density"`
}

type mpResponse struct {
	Data []mpDoc `json:"data"`
}

// Summary сводит любую ошибку к «unknown»: новизна лишь подсказка.
func (c *Client) Summary(ctx context.Context, formula string) (*MaterialSummary, error) {
	formula = strings.TrimSpace(formula)
	if c.apiKey == "" || formula == "" {
		return &MaterialSummary{
			Known: false, MaterialID: "", Formula: formula,
			IsStable: false, BandGap: 0, Density: 0,
		}, nil
	}

	endpoint := summaryEndpoint + "?" + url.Values{
		"formula": {formula},
		"_fields": {"material_id,formula_pretty,is_stable,band_gap,density"},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, errors.New("matproj: build request failed")
	}
	// MP ждёт X-API-KEY; имена заголовков регистронезависимы, каноническая форма равнозначна.
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return &MaterialSummary{
			Known: false, MaterialID: "", Formula: formula,
			IsStable: false, BandGap: 0, Density: 0,
		}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return &MaterialSummary{
			Known: false, MaterialID: "", Formula: formula,
			IsStable: false, BandGap: 0, Density: 0,
		}, nil
	}

	var body mpResponse
	if derr := jsonx.NewDecoder(resp.Body).Decode(&body); derr != nil || len(body.Data) == 0 {
		return &MaterialSummary{
			Known: false, MaterialID: "", Formula: formula,
			IsStable: false, BandGap: 0, Density: 0,
		}, nil
	}

	doc := body.Data[0]
	out := &MaterialSummary{
		Known: true, MaterialID: doc.MaterialID, Formula: doc.FormulaPret,
		IsStable: doc.IsStable, BandGap: derefFloat(doc.BandGap), Density: derefFloat(doc.Density),
	}
	if out.Formula == "" {
		out.Formula = formula
	}
	return out, nil
}

func (c *Client) Known(ctx context.Context, formula string) (bool, error) {
	s, err := c.Summary(ctx, formula)
	if err != nil {
		return false, err
	}
	return s.Known, nil
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
