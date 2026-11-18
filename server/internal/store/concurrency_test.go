package store

import (
	"fmt"
	"testing"
)

func TestConcurrentUserPatchesPreserveFields(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 8; i++ {
		u := testUser(t, s, fmt.Sprintf("concurrent-%d", i))
		name, avatar := "updated", "avatar"
		start, errs := make(chan struct{}), make(chan error, 2)
		for _, patch := range []UserPatch{{DisplayName: &name}, {Avatar: &avatar}} {
			go func() { <-start; _, err := s.UpdateUser(t.Context(), u.ID, patch); errs <- err }()
		}
		close(start)
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.GetUser(t.Context(), u.ID)
		if err != nil || got.DisplayName != name || got.Avatar != avatar {
			t.Fatalf("lost patch: %+v %v", got, err)
		}
	}
}

func TestConcurrentModelAppendsAndOrder(t *testing.T) {
	s := testStore(t)
	p := &Provider{Name: "provider"}
	if err := s.UpsertProvider(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	start, errs := make(chan struct{}), make(chan error, workers)
	for range workers {
		go func() {
			<-start
			errs <- s.CreateModels(t.Context(), []*Model{{ProviderID: p.ID}, {ProviderID: p.ID}})
		}()
	}
	close(start)
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	models, err := s.ListModels(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range models {
		if m.Sort != i {
			t.Fatalf("nonconsecutive sort: %+v", models)
		}
	}
	if len(models) != workers*2 {
		t.Fatalf("models=%d", len(models))
	}
	if err := s.SetModelOrder(t.Context(), nil); err != nil {
		t.Fatalf("empty order: %v", err)
	}
	ordered, err := s.ListModels(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range models {
		if ordered[i].ID != m.ID || ordered[i].Sort != m.Sort {
			t.Fatal("empty order changed ordering")
		}
	}
	ids := []string{models[len(models)-1].ID, models[0].ID}
	if err := s.SetModelOrder(t.Context(), ids); err != nil {
		t.Fatal(err)
	}
	ordered, err = s.ListModels(t.Context(), false)
	if err != nil || ordered[0].ID != ids[0] || ordered[1].ID != ids[1] {
		t.Fatalf("order: %+v %v", ordered, err)
	}
}
