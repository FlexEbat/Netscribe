package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func twoUsers(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	a, err := s.CreateUser(bg, model.UserRecord{User: model.User{Username: "ann", Role: model.RoleAdmin}, PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser(bg, model.UserRecord{User: model.User{Username: "bob", Role: model.RoleViewer}, PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID, b.ID
}

func TestFiltersBelongToTheirOwnerAndListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ann, bob := twoUsers(t, s)
	first, err := s.CreateFilter(bg, ann, model.PageInventory, "Offline", "status=offline")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.CreateFilter(bg, ann, model.PageInventory, "Unknown", "trust=unknown")
	if _, err := s.CreateFilter(bg, bob, model.PageInventory, "Mine", "q=x"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListFilters(bg, ann, model.PageInventory)
	if err != nil || len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("ann's list = %+v, %v, want newest first and only hers", got, err)
	}
	if got, _ := s.ListFilters(bg, ann, model.PageServices); len(got) != 0 {
		t.Errorf("other page: %+v", got)
	}
	if err := s.DeleteFilter(bg, bob, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob deleting ann's filter: %v, want ErrNotFound", err)
	}
	if got, _ := s.ListFilters(bg, ann, model.PageInventory); len(got) != 2 {
		t.Errorf("a refused delete removed something: %+v", got)
	}
	if err := s.DeleteFilter(bg, ann, first.ID); err != nil {
		t.Errorf("ann deleting her own: %v", err)
	}
}

func TestFilterNamesAreUniquePerPersonAndPageWithoutRegardToCase(t *testing.T) {
	s := newTestStore(t)
	ann, bob := twoUsers(t, s)
	if _, err := s.CreateFilter(bg, ann, model.PageInventory, "Offline", "status=offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFilter(bg, ann, model.PageInventory, "OFFLINE", "status=online"); !errors.Is(err, ErrConflict) {
		t.Errorf("same name in capitals: %v, want ErrConflict", err)
	}
	if _, err := s.CreateFilter(bg, ann, model.PageServices, "Offline", "q=ssh"); err != nil {
		t.Errorf("same name on another page: %v", err)
	}
	if _, err := s.CreateFilter(bg, bob, model.PageInventory, "Offline", "status=offline"); err != nil {
		t.Errorf("same name for another person: %v", err)
	}
}

func TestThe21stFilterOfAPageIsRefused(t *testing.T) {
	s := newTestStore(t)
	ann, _ := twoUsers(t, s)
	for i := 0; i < model.MaxFiltersPerPage; i++ {
		if _, err := s.CreateFilter(bg, ann, model.PageServices, fmt.Sprintf("f%d", i), "q=x"); err != nil {
			t.Fatalf("filter %d: %v", i, err)
		}
	}
	if _, err := s.CreateFilter(bg, ann, model.PageServices, "one too many", "q=x"); !errors.Is(err, ErrTooManyFilters) {
		t.Errorf("21st filter: %v, want ErrTooManyFilters", err)
	}
	if _, err := s.CreateFilter(bg, ann, model.PageContainers, "other page", "q=x"); err != nil {
		t.Errorf("the limit is per page: %v", err)
	}
}

func TestDeletingAUserDeletesTheirFilters(t *testing.T) {
	s := newTestStore(t)
	ann, bob := twoUsers(t, s)
	if _, err := s.CreateFilter(bg, bob, model.PageInventory, "Mine", "q=x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFilter(bg, ann, model.PageInventory, "Hers", "q=y"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(bg, bob); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got, _ := s.ListFilters(bg, bob, model.PageInventory); len(got) != 0 {
		t.Errorf("the deleted user's filters are still there: %+v", got)
	}
	if got, _ := s.ListFilters(bg, ann, model.PageInventory); len(got) != 1 {
		t.Errorf("ann's filter went with bob's: %+v", got)
	}
}
