package valkey_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/example/rag-mvp/pkg/valkey"
)

type payload struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func newClient(t *testing.T) *valkey.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := valkey.New(context.Background(), mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() {
		if cerr := c.Close(); cerr != nil {
			t.Errorf("close: %v", cerr)
		}
	})
	return c
}

func TestNewError(t *testing.T) {
	if _, err := valkey.New(context.Background(), "127.0.0.1:1", "", 0); err == nil {
		t.Fatal("expected an error connecting to a dead address")
	}
}

func TestPing(t *testing.T) {
	c := newClient(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestSetGet(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	if err := c.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || got != "v" {
		t.Fatalf("get = (%q, %v), want (\"v\", true)", got, ok)
	}
}

func TestGetMiss(t *testing.T) {
	c := newClient(t)
	got, ok, err := c.Get(context.Background(), "absent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok || got != "" {
		t.Fatalf("get miss = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestSetGetJSON(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	in := payload{Name: "owl", N: 7}
	if err := c.SetJSON(ctx, "j", in, time.Minute); err != nil {
		t.Fatalf("set json: %v", err)
	}
	var out payload
	ok, err := c.GetJSON(ctx, "j", &out)
	if err != nil {
		t.Fatalf("get json: %v", err)
	}
	if !ok || out != in {
		t.Fatalf("get json = (%+v, %v), want (%+v, true)", out, ok, in)
	}
}

func TestGetJSONMiss(t *testing.T) {
	c := newClient(t)
	var out payload
	ok, err := c.GetJSON(context.Background(), "absent", &out)
	if err != nil {
		t.Fatalf("get json: %v", err)
	}
	if ok {
		t.Fatal("expected a cache miss for an absent key")
	}
}

func TestGetJSONDecodeError(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	if err := c.Set(ctx, "bad", "{not json", time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	var out payload
	if _, err := c.GetJSON(ctx, "bad", &out); err == nil {
		t.Fatal("expected a decode error for malformed JSON")
	}
}

func TestSetJSONEncodeError(t *testing.T) {
	c := newClient(t)
	if err := c.SetJSON(context.Background(), "ch", make(chan int), time.Minute); err == nil {
		t.Fatal("expected an encode error for a channel value")
	}
}

func TestSetNX(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	ok, err := c.SetNX(ctx, "nx", "first", time.Minute)
	if err != nil {
		t.Fatalf("setnx: %v", err)
	}
	if !ok {
		t.Fatal("first SetNX should succeed")
	}
	ok, err = c.SetNX(ctx, "nx", "second", time.Minute)
	if err != nil {
		t.Fatalf("setnx existing: %v", err)
	}
	if ok {
		t.Fatal("SetNX on an existing key should return false")
	}
	got, _, err := c.Get(ctx, "nx")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "first" {
		t.Fatalf("value = %q, want %q", got, "first")
	}
}

func TestDel(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	if err := c.Set(ctx, "d1", "a", time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := c.Del(ctx, "d1", "d2"); err != nil {
		t.Fatalf("del: %v", err)
	}
	_, ok, err := c.Get(ctx, "d1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Fatal("deleted key should miss")
	}
}

func TestListOps(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	if err := c.LPush(ctx, "list", "a", "b", "c"); err != nil {
		t.Fatalf("lpush: %v", err)
	}
	got, err := c.LRange(ctx, "list", 0, -1)
	if err != nil {
		t.Fatalf("lrange: %v", err)
	}
	want := []string{"c", "b", "a"}
	if len(got) != len(want) {
		t.Fatalf("lrange = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lrange[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if terr := c.LTrim(ctx, "list", 0, 1); terr != nil {
		t.Fatalf("ltrim: %v", terr)
	}
	got, err = c.LRange(ctx, "list", 0, -1)
	if err != nil {
		t.Fatalf("lrange after trim: %v", err)
	}
	if len(got) != 2 || got[0] != "c" || got[1] != "b" {
		t.Fatalf("trimmed list = %v, want [c b]", got)
	}
}

func TestIncrTTLDecrGetInt(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	n, err := c.IncrTTL(ctx, "ctr", time.Minute)
	if err != nil {
		t.Fatalf("incr: %v", err)
	}
	if n != 1 {
		t.Fatalf("first incr = %d, want 1", n)
	}
	if n, err = c.IncrTTL(ctx, "ctr", 0); err != nil {
		t.Fatalf("incr no ttl: %v", err)
	}
	if n != 2 {
		t.Fatalf("second incr = %d, want 2", n)
	}
	if n, err = c.Decr(ctx, "ctr"); err != nil {
		t.Fatalf("decr: %v", err)
	}
	if n != 1 {
		t.Fatalf("decr = %d, want 1", n)
	}
	got, err := c.GetInt(ctx, "ctr")
	if err != nil {
		t.Fatalf("get int: %v", err)
	}
	if got != 1 {
		t.Fatalf("get int = %d, want 1", got)
	}
}

func TestGetIntMiss(t *testing.T) {
	c := newClient(t)
	got, err := c.GetInt(context.Background(), "absent")
	if err != nil {
		t.Fatalf("get int: %v", err)
	}
	if got != 0 {
		t.Fatalf("get int miss = %d, want 0", got)
	}
}

func TestGetIntParseError(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	if err := c.Set(ctx, "nan", "not-a-number", time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := c.GetInt(ctx, "nan"); err == nil {
		t.Fatal("expected a parse error for a non-numeric value")
	}
}

func TestOpsAfterClose(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := valkey.New(context.Background(), mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if cerr := c.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	ctx := context.Background()
	if perr := c.Ping(ctx); perr == nil {
		t.Error("ping after close should fail")
	}
	if serr := c.Set(ctx, "k", "v", time.Minute); serr == nil {
		t.Error("set after close should fail")
	}
	if _, _, gerr := c.Get(ctx, "k"); gerr == nil {
		t.Error("get after close should fail")
	}
	if _, gerr := c.GetJSON(ctx, "k", &payload{}); gerr == nil {
		t.Error("get json after close should fail")
	}
	if _, gerr := c.GetInt(ctx, "k"); gerr == nil {
		t.Error("get int after close should fail")
	}
	if _, nxerr := c.SetNX(ctx, "k", "v", time.Minute); nxerr == nil {
		t.Error("setnx after close should fail")
	}
	if derr := c.Del(ctx, "k"); derr == nil {
		t.Error("del after close should fail")
	}
	if perr := c.LPush(ctx, "l", "a"); perr == nil {
		t.Error("lpush after close should fail")
	}
	if terr := c.LTrim(ctx, "l", 0, 1); terr == nil {
		t.Error("ltrim after close should fail")
	}
	if _, rerr := c.LRange(ctx, "l", 0, -1); rerr == nil {
		t.Error("lrange after close should fail")
	}
	if _, ierr := c.IncrTTL(ctx, "c", time.Minute); ierr == nil {
		t.Error("incr after close should fail")
	}
	if _, derr := c.Decr(ctx, "c"); derr == nil {
		t.Error("decr after close should fail")
	}
}
