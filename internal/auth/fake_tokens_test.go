package auth

import (
	"context"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// The user and token half of fakeRepo.

func (r *fakeRepo) GetUser(_ context.Context, id int64) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.ID == id {
			return u.User, nil
		}
	}
	return model.User{}, model.ErrNotFound
}

func (r *fakeRepo) DeleteUser(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, u := range r.users {
		if u.ID == id {
			delete(r.users, name)
			return nil
		}
	}
	return model.ErrNotFound
}

func (r *fakeRepo) CreateToken(_ context.Context, t TokenRecord) (model.APIToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := 0
	for _, x := range r.tokens {
		if x.UserID == t.UserID && !x.Revoked && x.ExpiresAt.After(t.CreatedAt) {
			active++
		}
	}
	if active >= 20 {
		return model.APIToken{}, model.ErrConflict
	}
	r.nextID++
	t.ID = r.nextID
	r.tokens[t.Hash] = &t
	return model.APIToken{ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scope: t.Scope, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt}, nil
}

func (r *fakeRepo) GetTokenByHash(_ context.Context, hash string) (TokenRecord, model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[hash]
	if !ok {
		return TokenRecord{}, model.User{}, model.ErrNotFound
	}
	for _, u := range r.users {
		if u.ID == t.UserID {
			return *t, u.User, nil
		}
	}
	return TokenRecord{}, model.User{}, model.ErrNotFound
}

func (r *fakeRepo) ListTokens(_ context.Context, userID int64) ([]model.APIToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []model.APIToken{}
	for _, t := range r.tokens {
		if t.UserID == userID {
			out = append(out, model.APIToken{ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scope: t.Scope, Revoked: t.Revoked})
		}
	}
	return out, nil
}

func (r *fakeRepo) RevokeToken(_ context.Context, id, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tokens {
		if t.ID == id && t.UserID == userID {
			t.Revoked = true
			return nil
		}
	}
	return model.ErrNotFound
}

func (r *fakeRepo) TouchToken(_ context.Context, id int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tokens {
		if t.ID == id {
			t.LastUsedAt = &now
			r.tokenTouches++
		}
	}
	return nil
}
