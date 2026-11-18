package store

import (
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestBind(t *testing.T) {
	for _, q := range []struct{ in, out string }{
		{`SELECT ?, ?, '?'`, `SELECT $1, $2, '?'`},
		{`SELECT 'it''s ?' WHERE a = ? AND b LIKE ? ESCAPE '\' AND c = ?`, `SELECT 'it''s ?' WHERE a = $1 AND b LIKE $2 ESCAPE '\' AND c = $3`},
		{`SELECT json_extract(j.value, '$.file_id') WHERE id = ?`, `SELECT json_extract(j.value, '$.file_id') WHERE id = $1`},
	} {
		for i := 0; i < 2; i++ {
			if got := bind(q.in); got != q.out {
				t.Errorf("bind(%q)=%q want %q", q.in, got, q.out)
			}
		}
	}
}

func TestPrepare(t *testing.T) {
	args := []any{true, false, int64(3), "?"}
	s := &Store{pg: true}
	q, got := s.prepare(`SELECT ?,?,?,?`, args)
	if q != `SELECT $1,$2,$3,$4` || !reflect.DeepEqual(got, []any{int64(1), int64(0), int64(3), "?"}) {
		t.Fatalf("prepare: %s %v", q, got)
	}
	if !reflect.DeepEqual(args, []any{true, false, int64(3), "?"}) {
		t.Fatal("prepare modified caller args")
	}
	if !isUnique(&pgconn.PgError{Code: "23505"}) || isUnique(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("incorrect unique constraint detection")
	}
}

func TestIntegerAndAggregateScans(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	p := &Provider{Name: "provider", Enabled: true}
	if err := s.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false, true} {
		if err := s.UpsertModel(ctx, &Model{ProviderID: p.ID, Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetProvider(ctx, p.ID)
	if err != nil || !got.Enabled {
		t.Fatalf("bool scan: %+v %v", got, err)
	}
	for _, dest := range []any{new(int), new(int64), new(float64)} {
		if err := s.queryRow(ctx, `SELECT SUM(enabled) FROM models`).Scan(dest); err != nil {
			t.Fatalf("SUM scan into %T: %v", dest, err)
		}
		if reflect.ValueOf(dest).Elem().Convert(reflect.TypeOf(float64(0))).Float() != 2 {
			t.Fatalf("SUM=%v", dest)
		}
	}
	counts, err := s.ModelCounts(ctx)
	if err != nil || counts[p.ID] != [2]int{3, 2} {
		t.Fatalf("counts=%v err=%v", counts, err)
	}
}
