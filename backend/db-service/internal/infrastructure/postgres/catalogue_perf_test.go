package postgres_test

import (
	"context"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/extractdoc"
)

// Нужен p95 для десяти читателей; OPS07_DSN указывать только на базу для замеров.
const (
	perfClients    = 10
	perfPerClient  = 20
	perfReader     = "00000000-0000-0000-0000-0000000000ff"
	perfPageBudget = 2 * time.Second
	perfCardBudget = 3 * time.Second
)

func TestCataloguePerf(t *testing.T) {
	dsn := os.Getenv("OPS07_DSN")
	if dsn == "" {
		t.Skip("set OPS07_DSN to the throwaway OPS-07 corpus to run the catalogue load test")
	}
	db, err := postgres.Connect(context.Background(), dsn, zerolog.Nop())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	repo := postgres.NewTechnologyRepo(db).WithSharedCorpus(false)

	page := func(query string, all bool) func(context.Context) error {
		return func(ctx context.Context) error {
			_, perr := repo.SearchTechnologies(ctx, extractdoc.SearchTechnologies{
				ReaderID: perfReader, Query: query, Limit: 20, IncludeAll: all})
			return perr
		}
	}
	first := catalogueFirstID(t, repo)
	card := func(ctx context.Context) error {
		_, perr := repo.ReadTechnology(ctx, extractdoc.ReadTechnology{
			ReaderID: perfReader, TechnologyID: first})
		return perr
	}

	for _, c := range []struct {
		name   string
		budget time.Duration
		run    func(context.Context) error
	}{
		{"общий термин", perfPageBudget, page("сепарация", false)},
		{"узкий термин", perfPageBudget, page("запись 4.", false)},
		{"только синоним", perfPageBudget, page("alias routing 5", false)},
		{"весь каталог", perfPageBudget, page("", true)},
		{"худший случай", perfPageBudget, page("метод решает задачу", false)},
		{"карточка", perfCardBudget, card},
	} {
		p50, p95, p99 := perfPercentiles(t, c.run)
		t.Logf("%-16s p50 %6.3f с  p95 %6.3f с  p99 %6.3f с  порог %s",
			c.name, p50.Seconds(), p95.Seconds(), p99.Seconds(), c.budget)
		if p95 > c.budget {
			t.Errorf("%s: p95 %s превышает порог %s", c.name, p95, c.budget)
		}
	}
}

func catalogueFirstID(t *testing.T, repo *postgres.TechnologyRepo) string {
	t.Helper()
	found, err := repo.SearchTechnologies(t.Context(), extractdoc.SearchTechnologies{
		ReaderID: perfReader, Limit: 1, IncludeAll: true})
	if err != nil || len(found.Technologies) == 0 {
		t.Fatalf("the corpus is empty, so there is nothing to measure: %v %+v", err, found)
	}
	return found.Technologies[0].TechnologyID
}

func perfPercentiles(t *testing.T, run func(context.Context) error) (p50, p95, p99 time.Duration) {
	t.Helper()
	if err := run(t.Context()); err != nil { // прогрев: план и кэш
		t.Fatalf("warm-up: %v", err)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	took := make([]time.Duration, 0, perfClients*perfPerClient)
	for range perfClients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perfPerClient {
				start := time.Now()
				err := run(t.Context())
				elapsed := time.Since(start)
				mu.Lock()
				if err != nil {
					t.Errorf("call: %v", err)
				}
				took = append(took, elapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	slices.Sort(took)
	at := func(q float64) time.Duration { return took[min(int(float64(len(took))*q), len(took)-1)] }
	return at(0.50), at(0.95), at(0.99)
}
