package postgres_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/analysisdoc"
)

// Одновременно пишут две реплики: проверка версии должна защищать и отсутствующую строку, и её записи источников.
func TestLiveSearchConditionalSave(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewAnalysisRepo(db)
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing-expired=%v", existing), func(t *testing.T) {
			query := "cas-test|" + uuid.NewV4().String()
			t.Cleanup(func() {
				if _, err := db.Pool.Exec(context.Background(), "DELETE FROM trend_live_searches WHERE query=$1", query); err != nil {
					t.Error(err)
				}
			})
			version := ""
			if existing {
				seed := liveCASRequest(query, "seed", new(""))
				seed.ExpiresAt = time.Now().Add(-time.Hour)
				saved, err := repo.SaveLiveSearch(t.Context(), seed)
				if err != nil {
					t.Fatal(err)
				}
				read, err := repo.ReadLiveSearch(t.Context(), analysisdoc.LiveSearchRead{Query: query, WithVersion: true})
				if err != nil || !read.Found || read.Fresh || read.Version == "" || read.Version != saved.Version {
					t.Fatalf("expired snapshot lost its version: %+v %v", read, err)
				}
				version = read.Version
			}
			winner := raceLiveSearchSaves(t, repo, query, version)
			read, err := repo.ReadLiveSearch(t.Context(), analysisdoc.LiveSearchRead{Query: query, WithVersion: true})
			if err != nil || !read.Found || read.Version != winner.Version || read.Sources != 1 {
				t.Fatalf("ack did not identify the committed snapshot: %+v %v", read, err)
			}
			var detail struct {
				Run string `json:"run"`
			}
			if err = json.Unmarshal(read.Detail, &detail); err != nil {
				t.Fatal(err)
			}
			var title string
			if err = db.Pool.QueryRow(t.Context(), "SELECT title FROM trend_live_records WHERE query=$1", query).Scan(&title); err != nil {
				t.Fatal(err)
			}
			if title != detail.Run {
				t.Fatalf("losing writer replaced the winner's source records: detail=%s title=%s", detail.Run, title)
			}
			_, err = repo.SaveLiveSearch(t.Context(), liveCASRequest(query, "stale", new(version)))
			if !errors.Is(err, analysisdoc.ErrConflict) {
				t.Fatalf("stale retry was accepted: %v", err)
			}
			unchanged, err := repo.ReadLiveSearch(t.Context(), analysisdoc.LiveSearchRead{Query: query, WithVersion: true})
			if err != nil || unchanged.Version != read.Version || unchanged.Sources != 1 {
				t.Fatalf("rejected retry changed the snapshot: %+v %v", unchanged, err)
			}
			// Старые клиенты сохраняют прежний контракт, но тоже участвуют в блокировке ключа.
			legacy, err := repo.SaveLiveSearch(t.Context(), liveCASRequest(query, "legacy", nil))
			if err != nil || legacy.Version != "" {
				t.Fatalf("legacy unconditional save failed: %+v %v", legacy, err)
			}
			legacy, err = repo.ReadLiveSearch(t.Context(), analysisdoc.LiveSearchRead{Query: query})
			if err != nil || !legacy.Found || legacy.Version != "" {
				t.Fatalf("legacy reader received the new response field: %+v %v", legacy, err)
			}
		})
	}
}

func liveCASRequest(query, run string, version *string) analysisdoc.LiveSearchSave {
	return analysisdoc.LiveSearchSave{
		Query: query, TermsKey: query, IfMatch: version,
		ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		Detail:  []byte(fmt.Sprintf("{\n\"run\": %q\n}", run)),
		Records: []analysisdoc.LiveSearchRecord{{URL: "https://example.org/" + run, Channel: "science", Title: run}},
	}
}

func TestLiveSearchFreshRunSupersedesOnlyBaselineAuxiliaryChanges(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewAnalysisRepo(db)
	query := "cas-test|" + uuid.NewV4().String()
	t.Cleanup(func() {
		if _, err := db.Pool.Exec(context.Background(), "DELETE FROM trend_live_searches WHERE query=$1", query); err != nil {
			t.Error(err)
		}
	})
	request := func(run, digest string, version *string) analysisdoc.LiveSearchSave {
		req := liveCASRequest(query, run, version)
		req.Detail = []byte(fmt.Sprintf(`{"run_id":%q,"digest":%q}`, run, digest))
		return req
	}
	initial, err := repo.SaveLiveSearch(t.Context(), request("a", "", new("")))
	if err != nil {
		t.Fatal(err)
	}
	aux, err := repo.SaveLiveSearch(t.Context(), request("a", "complete", new(initial.Version)))
	if err != nil || aux.Version == initial.Version {
		t.Fatalf("auxiliary update did not commit: %+v %v", aux, err)
	}
	fresh := request("b", "", new(initial.Version))
	fresh.BaselineRunID = "a"
	winner, err := repo.SaveLiveSearch(t.Context(), fresh)
	if err != nil || winner.Version == aux.Version {
		t.Fatalf("old auxiliary update displaced a fresh run: %+v %v", winner, err)
	}
	for _, mode := range []string{"old-aux", "other-fresh", "same-run", "absent"} {
		stale := request("a", "late", new(initial.Version))
		switch mode {
		case "other-fresh":
			stale = request("c", "", new(initial.Version))
			stale.BaselineRunID = "a"
		case "same-run":
			stale = request("b", "late", new(aux.Version))
			stale.BaselineRunID = "b"
		case "absent":
			stale = request("c", "", new(""))
			stale.BaselineRunID = "b"
		}
		if _, saveErr := repo.SaveLiveSearch(t.Context(), stale); !errors.Is(saveErr, analysisdoc.ErrConflict) {
			t.Fatalf("%s bypassed the precondition: %v", mode, saveErr)
		}
	}
	current, err := repo.ReadLiveSearch(t.Context(), analysisdoc.LiveSearchRead{Query: query, WithVersion: true})
	if err != nil || current.Version != winner.Version {
		t.Fatalf("a stale writer changed the winning run: %+v %v", current, err)
	}
}

func raceLiveSearchSaves(t *testing.T, repo *postgres.AnalysisRepo, query, version string) analysisdoc.LiveSearchStored {
	t.Helper()
	type result struct {
		saved analysisdoc.LiveSearchStored
		err   error
	}
	start, results := make(chan struct{}), make(chan result, 2)
	for _, run := range []string{"a", "b"} {
		go func() {
			<-start
			saved, err := repo.SaveLiveSearch(t.Context(), liveCASRequest(query, run, new(version)))
			results <- result{saved: saved, err: err}
		}()
	}
	close(start)
	var winner analysisdoc.LiveSearchStored
	var committed, conflicts int
	for range 2 {
		got := <-results
		switch {
		case got.err == nil:
			winner = got.saved
			committed++
		case errors.Is(got.err, analysisdoc.ErrConflict):
			conflicts++
		default:
			t.Fatal(got.err)
		}
	}
	if committed != 1 || conflicts != 1 || winner.Version == "" {
		t.Fatalf("conditional writes were not atomic: committed=%d conflicts=%d version=%q", committed, conflicts, winner.Version)
	}
	return winner
}
