package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestUserRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(db)

	before, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	u := &domain.User{
		ID: uuid.NewV4().String(), Username: "user-" + uuid.NewV4().String(), PasswordHash: "h",
		Roles: []string{"admin", "user"}, CreatedAt: time.Now().UTC(),
	}
	if cerr := repo.Create(ctx, u); cerr != nil {
		t.Fatalf("create: %v", cerr)
	}

	got, err := repo.GetByUsername(ctx, u.Username)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != u.ID || len(got.Roles) != 2 || got.Roles[0] != "admin" {
		t.Fatalf("user not round-tripped: %+v", got)
	}

	after, err := repo.Count(ctx)
	if err != nil || after != before+1 {
		t.Fatalf("count after insert: before=%d after=%d err=%v", before, after, err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, x := range list {
		if x.ID == u.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created user not in listing (n=%d)", len(list))
	}

	if _, gerr := repo.GetByUsername(ctx, "missing-"+uuid.NewV4().String()); !errors.Is(gerr, domain.ErrNotFound) {
		t.Fatalf("get missing: got %v, want ErrNotFound", gerr)
	}

	u2 := &domain.User{
		ID: uuid.NewV4().String(), Username: "u2-" + uuid.NewV4().String(), PasswordHash: "h", CreatedAt: time.Now().UTC(),
	}
	if cerr := repo.Create(ctx, u2); cerr != nil {
		t.Fatalf("create nil-roles: %v", cerr)
	}
	got2, err := repo.GetByUsername(ctx, u2.Username)
	if err != nil || got2.Roles == nil || len(got2.Roles) != 0 {
		t.Fatalf("nil roles not stored as empty array: %+v err=%v", got2, err)
	}

	dup := &domain.User{ID: uuid.NewV4().String(), Username: u.Username, PasswordHash: "x", CreatedAt: time.Now().UTC()}
	if cerr := repo.Create(ctx, dup); cerr == nil {
		t.Fatal("duplicate username should violate the UNIQUE constraint")
	}
}

func TestDocumentRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewDocumentRepo(db)
	owner := uuid.NewV4().String()

	d := &domain.Document{
		ID: uuid.NewV4().String(), OwnerID: owner, Filename: "paper.pdf", MIMEType: "application/pdf",
		Size: 1234, ObjectKey: "k/" + uuid.NewV4().String(), Status: "uploaded", ContentHash: "hash-" + uuid.NewV4().String(),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if cerr := repo.Create(ctx, d); cerr != nil {
		t.Fatalf("create: %v", cerr)
	}

	got, err := repo.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Filename != "paper.pdf" || got.Size != 1234 || got.Status != "uploaded" {
		t.Fatalf("document not round-tripped: %+v", got)
	}

	if _, gerr := repo.Get(ctx, uuid.NewV4().String()); !errors.Is(gerr, domain.ErrNotFound) {
		t.Fatalf("get missing: got %v, want ErrNotFound", gerr)
	}

	cc := 17
	if uerr := repo.UpdateStatus(ctx, d.ID, "indexed", "done", &cc); uerr != nil {
		t.Fatalf("update status: %v", uerr)
	}
	got, err = repo.Get(ctx, d.ID)
	if err != nil || got.Status != "indexed" || got.ChunkCount != 17 || got.StatusMsg != "done" {
		t.Fatalf("status not persisted: %+v err=%v", got, err)
	}
	// nil в chunk count сохраняет прежнее значение (COALESCE).
	if uerr := repo.UpdateStatus(ctx, d.ID, "parsed", "", nil); uerr != nil {
		t.Fatalf("update status nil chunk: %v", uerr)
	}
	got, err = repo.Get(ctx, d.ID)
	if err != nil || got.Status != "indexed" || got.ChunkCount != 17 {
		t.Fatalf("late stage must preserve terminal state and count: %+v err=%v", got, err)
	}

	byOwner, err := repo.ListByOwner(ctx, owner)
	if err != nil || len(byOwner) != 1 || byOwner[0].ID != d.ID {
		t.Fatalf("list by owner: %+v err=%v", byOwner, err)
	}
	all, err := repo.ListByOwner(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	found := false
	for _, x := range all {
		if x.ID == d.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list-all did not include the document (n=%d)", len(all))
	}
}

func TestDocumentFindByHash(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewDocumentRepo(db)
	owner := uuid.NewV4().String()
	hash := "h-" + uuid.NewV4().String()

	mk := func(status string, createdAt time.Time) *domain.Document {
		return &domain.Document{
			ID: uuid.NewV4().String(), OwnerID: owner, ObjectKey: "k/" + uuid.NewV4().String(),
			Status: status, ContentHash: hash, CreatedAt: createdAt, UpdatedAt: createdAt,
		}
	}
	old := mk("indexed", time.Now().UTC().Add(-time.Hour))
	newest := mk("indexed", time.Now().UTC())
	failed := mk("failed", time.Now().UTC().Add(time.Hour))
	for _, dd := range []*domain.Document{old, newest, failed} {
		if cerr := repo.Create(ctx, dd); cerr != nil {
			t.Fatalf("seed: %v", cerr)
		}
	}

	got, err := repo.FindByHash(ctx, "", hash)
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if got.ID != newest.ID {
		t.Fatalf("expected newest non-failed match %s, got %s", newest.ID, got.ID)
	}

	if _, ferr := repo.FindByHash(ctx, "", "absent-"+uuid.NewV4().String()); !errors.Is(ferr, domain.ErrNotFound) {
		t.Fatalf("absent hash: got %v, want ErrNotFound", ferr)
	}
}

func TestChatRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewChatRepo(db)
	owner := uuid.NewV4().String()

	c := &domain.Chat{ID: uuid.NewV4().String(), OwnerID: owner, Title: "Тема", CreatedAt: time.Now().UTC()}
	if cerr := repo.CreateChat(ctx, c); cerr != nil {
		t.Fatalf("create chat: %v", cerr)
	}

	got, err := repo.GetChat(ctx, c.ID)
	if err != nil || got.Title != "Тема" {
		t.Fatalf("get chat: %+v err=%v", got, err)
	}
	if _, gerr := repo.GetChat(ctx, uuid.NewV4().String()); !errors.Is(gerr, domain.ErrNotFound) {
		t.Fatalf("get missing chat: got %v, want ErrNotFound", gerr)
	}

	chats, total, err := repo.ListChats(ctx, owner, 0, 0, false)
	if err != nil || total != 1 || len(chats) != 1 || chats[0].ID != c.ID {
		t.Fatalf("list chats: %+v total=%d err=%v", chats, total, err)
	}

	now := time.Now().UTC()
	m1 := &domain.Message{
		ID: uuid.NewV4().String(), ChatID: c.ID, Role: "user", Content: "вопрос",
		Sources: []byte(`[{"id":"s1"}]`), CreatedAt: now,
	}
	m2 := &domain.Message{
		ID: uuid.NewV4().String(), ChatID: c.ID, Role: "assistant", Content: "ответ", CreatedAt: now.Add(time.Second),
	}
	for _, m := range []*domain.Message{m1, m2} {
		if aerr := repo.AddMessage(ctx, m); aerr != nil {
			t.Fatalf("add message: %v", aerr)
		}
	}

	msgs, err := repo.ListMessages(ctx, c.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != m1.ID || msgs[1].ID != m2.ID {
		t.Fatalf("messages not in chronological order: %+v", msgs)
	}
	// PostgreSQL пересериализует JSONB (пробелы другие), поэтому сверяем по содержимому.
	if !strings.Contains(string(msgs[0].Sources), `"s1"`) || string(msgs[1].Sources) != "[]" {
		t.Fatalf("sources not round-tripped/defaulted: %q %q", msgs[0].Sources, msgs[1].Sources)
	}

	if aerr := repo.AddMessage(ctx, &domain.Message{
		ID: uuid.NewV4().String(), ChatID: uuid.NewV4().String(), Role: "user", Content: "x", CreatedAt: now,
	}); aerr == nil {
		t.Fatal("message against missing chat should violate the FK")
	}
}

func TestModelRepo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewModelRepo(db)

	id := "model-" + uuid.NewV4().String()
	m := &domain.Model{ID: id, Name: "First", Role: "generation", Backend: "vLLM"}
	if uerr := repo.Upsert(ctx, m); uerr != nil {
		t.Fatalf("insert: %v", uerr)
	}
	m.Name = "Renamed"
	m.Backend = "llama.cpp"
	if uerr := repo.Upsert(ctx, m); uerr != nil {
		t.Fatalf("update: %v", uerr)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got *domain.Model
	for _, x := range list {
		if x.ID == id {
			got = x
		}
	}
	if got == nil || got.Name != "Renamed" || got.Backend != "llama.cpp" {
		t.Fatalf("upsert did not update in place: %+v", got)
	}
}

func TestPing(t *testing.T) {
	db := newTestDB(t)
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// Закрытый заранее пул проводит тест по всем веткам ошибок.
func TestRepoErrorsOnClosedPool(t *testing.T) {
	db := newTestDB(t)
	db.Close()
	ctx := context.Background()
	id := uuid.NewV4().String()

	// Конкретная ошибка зависит от драйвера; требуем лишь, чтобы она была.
	checks := []struct {
		name string
		fn   func() error
	}{
		{"user.create", func() error {
			return postgres.NewUserRepo(db).Create(ctx, &domain.User{ID: id, Username: id})
		}},
		{"user.list", func() error { _, err := postgres.NewUserRepo(db).List(ctx); return err }},
		{"user.count", func() error { _, err := postgres.NewUserRepo(db).Count(ctx); return err }},
		{"user.get", func() error { _, err := postgres.NewUserRepo(db).GetByUsername(ctx, id); return err }},
		{"doc.create", func() error {
			return postgres.NewDocumentRepo(db).Create(ctx, &domain.Document{ID: id})
		}},
		{"doc.get", func() error { _, err := postgres.NewDocumentRepo(db).Get(ctx, id); return err }},
		{"doc.findbyhash", func() error { _, err := postgres.NewDocumentRepo(db).FindByHash(ctx, "", id); return err }},
		{"doc.list", func() error { _, err := postgres.NewDocumentRepo(db).ListByOwner(ctx, id); return err }},
		{"doc.status", func() error { return postgres.NewDocumentRepo(db).UpdateStatus(ctx, id, "x", "", nil) }},
		{"chat.create", func() error { return postgres.NewChatRepo(db).CreateChat(ctx, &domain.Chat{ID: id}) }},
		{"chat.get", func() error { _, err := postgres.NewChatRepo(db).GetChat(ctx, id); return err }},
		{"chat.list", func() error { _, _, err := postgres.NewChatRepo(db).ListChats(ctx, id, 0, 0, false); return err }},
		{"chat.addmsg", func() error {
			return postgres.NewChatRepo(db).AddMessage(ctx, &domain.Message{ID: id, ChatID: id})
		}},
		{"chat.listmsg", func() error { _, err := postgres.NewChatRepo(db).ListMessages(ctx, id); return err }},
		{"model.list", func() error { _, err := postgres.NewModelRepo(db).List(ctx); return err }},
		{"model.upsert", func() error { return postgres.NewModelRepo(db).Upsert(ctx, &domain.Model{ID: id}) }},
		{"kpi.create", func() error { return postgres.NewKPIRepo(db).Create(ctx, &domain.KPI{ID: id}) }},
		{"kpi.get", func() error { _, err := postgres.NewKPIRepo(db).Get(ctx, id); return err }},
		{"kpi.list", func() error { _, err := postgres.NewKPIRepo(db).ListByOwner(ctx, id); return err }},
		{"kpi.update", func() error { return postgres.NewKPIRepo(db).Update(ctx, &domain.KPI{ID: id}) }},
		{"cluster.create", func() error { return postgres.NewClusterRepo(db).Create(ctx, &domain.Cluster{ID: id}) }},
		{"cluster.get", func() error { _, err := postgres.NewClusterRepo(db).Get(ctx, id); return err }},
		{"cluster.list", func() error { _, err := postgres.NewClusterRepo(db).ListByOwner(ctx, id); return err }},
		{"cluster.update", func() error { return postgres.NewClusterRepo(db).Update(ctx, &domain.Cluster{ID: id}) }},
		{"cluster.delete", func() error { _, err := postgres.NewClusterRepo(db).DeleteByOwner(ctx, id); return err }},
		{"hyp.create", func() error {
			return postgres.NewHypothesisRepo(db).Create(ctx, &domain.Hypothesis{ID: id}, nil)
		}},
		{"hyp.get", func() error { _, err := postgres.NewHypothesisRepo(db).Get(ctx, id); return err }},
		{"hyp.list", func() error {
			_, err := postgres.NewHypothesisRepo(db).List(ctx, domain.HypothesisFilter{OwnerID: id})
			return err
		}},
		{"hyp.update", func() error {
			return postgres.NewHypothesisRepo(db).Update(ctx, &domain.Hypothesis{ID: id}, nil)
		}},
		{"hyp.listevidence", func() error { _, err := postgres.NewHypothesisRepo(db).ListEvidence(ctx, id); return err }},
		{"hyp.addrevision", func() error {
			return postgres.NewHypothesisRepo(db).AddRevision(ctx, &domain.Revision{ID: id, HypothesisID: id, Action: "created"})
		}},
		{"hyp.listrevisions", func() error {
			_, err := postgres.NewHypothesisRepo(db).ListRevisions(ctx, id)
			return err
		}},
	}
	for _, c := range checks {
		if err := c.fn(); err == nil {
			t.Fatalf("%s on a closed pool should error", c.name)
		}
	}
}

func TestKPIRepoUpdateNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewKPIRepo(db)

	if _, gerr := repo.Get(ctx, uuid.NewV4().String()); !errors.Is(gerr, domain.ErrNotFound) {
		t.Fatalf("get missing kpi: got %v, want ErrNotFound", gerr)
	}
	missing := &domain.KPI{ID: uuid.NewV4().String(), Title: "t", Direction: "increase", Status: "active"}
	if uerr := repo.Update(ctx, missing); !errors.Is(uerr, domain.ErrNotFound) {
		t.Fatalf("update missing kpi: got %v, want ErrNotFound", uerr)
	}
}

func TestClusterRepoUpdateAndDelete(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := postgres.NewClusterRepo(db)
	owner := uuid.NewV4().String()

	if _, gerr := repo.Get(ctx, uuid.NewV4().String()); !errors.Is(gerr, domain.ErrNotFound) {
		t.Fatalf("get missing cluster: got %v, want ErrNotFound", gerr)
	}

	c := &domain.Cluster{
		ID: uuid.NewV4().String(), OwnerID: owner, Label: "Коррозия", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if cerr := repo.Create(ctx, c); cerr != nil {
		t.Fatalf("create: %v", cerr)
	}

	c.Label = "Коррозия НКТ"
	c.Status = "archived"
	c.Keywords = []string{"коррозия"}
	if uerr := repo.Update(ctx, c); uerr != nil {
		t.Fatalf("update: %v", uerr)
	}
	got, err := repo.Get(ctx, c.ID)
	if err != nil || got.Label != "Коррозия НКТ" || got.Status != "archived" || len(got.Keywords) != 1 {
		t.Fatalf("update not persisted: %+v err=%v", got, err)
	}

	missing := &domain.Cluster{ID: uuid.NewV4().String(), Label: "x", Status: "active"}
	if uerr := repo.Update(ctx, missing); !errors.Is(uerr, domain.ErrNotFound) {
		t.Fatalf("update missing cluster: got %v, want ErrNotFound", uerr)
	}

	listed, err := repo.ListByOwner(ctx, owner)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list by owner: %+v err=%v", listed, err)
	}

	n, err := repo.DeleteByOwner(ctx, owner)
	if err != nil || n != 1 {
		t.Fatalf("delete by owner: n=%d err=%v", n, err)
	}
	if _, gerr := repo.Get(ctx, c.ID); !errors.Is(gerr, domain.ErrNotFound) {
		t.Fatalf("cluster should be gone: got %v, want ErrNotFound", gerr)
	}
	if n2, derr := repo.DeleteByOwner(ctx, owner); derr != nil || n2 != 0 {
		t.Fatalf("delete again: n=%d err=%v", n2, derr)
	}
}
