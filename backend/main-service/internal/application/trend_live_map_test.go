package application

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeMapOpenAlex struct {
	query url.Values
}

func (f *fakeMapOpenAlex) Get(_ context.Context, _ string, query url.Values) ([]byte, error) {
	f.query = query
	return []byte(`{"meta":{"count":217},"group_by":[{"key":"https://openalex.org/countries/CN","count":81},` +
		`{"key":"US","count":43},{"key":"unknown","count":9}]}`), nil
}

func TestLiveMapCountriesWindow(t *testing.T) {
	from, mid, to := liveMapWindows(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	if from != "2025-10-01" || mid != "2026-04-01" || to != "2026-10-01" {
		t.Fatalf("окна: %s %s %s", from, mid, to)
	}
	client := &fakeMapOpenAlex{}
	window, err := liveMapCountries(context.Background(), client, `Long-duration "energy" storage, grid`, mid, to)
	if err != nil {
		t.Fatal(err)
	}
	want := `title_and_abstract.search:"Long-duration energy storage grid",` +
		`from_publication_date:2026-04-01,to_publication_date:2026-09-30`
	if got := client.query.Get("filter"); got != want {
		t.Fatalf("фильтр: %s", got)
	}
	if window.Total != 217 || window.Countries["CN"] != 81 || window.Countries["US"] != 43 || len(window.Countries) != 2 {
		t.Fatalf("страны: %+v", window)
	}
}

func TestLiveMapDiverseTakesOneTopicPerIndustryFirst(t *testing.T) {
	topics := []LiveTopic{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
	industry := map[string]string{"a": "computing", "b": "computing", "c": "medicine", "d": "computing"}
	got := make([]string, 0, len(topics))
	for _, topic := range liveMapDiverse(topics, func(topic LiveTopic) string { return industry[topic.Name] }) {
		got = append(got, topic.Name)
	}
	if strings.Join(got, "") != "acbd" {
		t.Fatalf("порядок %v, а нужен a c b d", got)
	}
}
