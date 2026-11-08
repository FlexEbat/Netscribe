package store

import (
	"context"
	"errors"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const idpIssuer = "https://idp.example.com"

func newIdentityUser(name string) model.UserRecord {
	return model.UserRecord{User: model.User{Username: name, DisplayName: "Person " + name, Role: model.RoleViewer}}
}

func TestIdentityUserIsCreatedFoundAndHasNoPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.FindIdentityUser(ctx, idpIssuer, "sub-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown identity: got %v, want ErrNotFound", err)
	}
	made, err := s.CreateIdentityUser(ctx, newIdentityUser("jane-1a2b3c4d"), idpIssuer, "sub-1", "jane@example.com")
	if err != nil {
		t.Fatalf("CreateIdentityUser: %v", err)
	}
	got, err := s.FindIdentityUser(ctx, idpIssuer, "sub-1")
	if err != nil || got.ID != made.ID || got.Username != "jane-1a2b3c4d" || got.Role != model.RoleViewer {
		t.Fatalf("found %+v, %v", got, err)
	}
	// The same subject at another issuer is another person.
	if _, err := s.FindIdentityUser(ctx, "https://other.example.com", "sub-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another issuer: got %v, want ErrNotFound", err)
	}
	rec, err := s.GetUserByName(ctx, "jane-1a2b3c4d")
	if err != nil || rec.PasswordHash != "" {
		t.Fatalf("the account must have an empty password hash: %q, %v", rec.PasswordHash, err)
	}
}

func TestIdentityConflictsLeaveNothingBehind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateIdentityUser(ctx, newIdentityUser("first-aaaaaaaa"), idpIssuer, "sub-1", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	// A taken username.
	if _, err := s.CreateIdentityUser(ctx, newIdentityUser("first-aaaaaaaa"), idpIssuer, "sub-2", "b@example.com"); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken username: got %v, want ErrConflict", err)
	}
	// A taken identity under a new username: the user row of this try must be rolled back.
	if _, err := s.CreateIdentityUser(ctx, newIdentityUser("second-bbbbbbbb"), idpIssuer, "sub-1", "c@example.com"); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken identity: got %v, want ErrConflict", err)
	}
	if n, err := s.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("users = %d, %v; a failed create must leave no user", n, err)
	}
}

func TestDeletingTheUserDeletesItsIdentity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	made, err := s.CreateIdentityUser(ctx, newIdentityUser("gone-cccccccc"), idpIssuer, "sub-9", "g@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, made.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindIdentityUser(ctx, idpIssuer, "sub-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the identity outlived its user: %v", err)
	}
	// The identity can be used again by a new account.
	if _, err := s.CreateIdentityUser(ctx, newIdentityUser("back-dddddddd"), idpIssuer, "sub-9", "g@example.com"); err != nil {
		t.Fatalf("reuse after delete: %v", err)
	}
}
