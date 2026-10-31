package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// ErrTooManyFilters says that a person already keeps the most filters for a page.
var ErrTooManyFilters = errors.New("too many saved filters")

func toFilter(r dbgen.SavedFilter) (model.SavedFilter, error) {
	at, err := parseTime(r.CreatedAt)
	if err != nil {
		return model.SavedFilter{}, fmt.Errorf("saved filter %d: %w", r.ID, err)
	}
	return model.SavedFilter{ID: r.ID, Page: r.Page, Name: r.Name, Query: r.Query, CreatedAt: at}, nil
}

// CreateFilter stores a checked filter for a person. A name that the person already used on
// the page is ErrConflict; the 21st filter of a page is ErrTooManyFilters.
func (s *Store) CreateFilter(ctx context.Context, userID int64, page, name, query string) (model.SavedFilter, error) {
	var out model.SavedFilter
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		n, err := q.CountFilters(ctx, dbgen.CountFiltersParams{UserID: userID, Page: page})
		if err != nil {
			return err
		}
		if n >= model.MaxFiltersPerPage {
			return ErrTooManyFilters
		}
		row, err := q.InsertFilter(ctx, dbgen.InsertFilterParams{
			UserID: userID, Page: page, Name: name, Query: query, CreatedAt: formatTime(s.now()),
		})
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		out, err = toFilter(row)
		return err
	})
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrTooManyFilters) {
		return model.SavedFilter{}, err
	}
	if err != nil {
		return model.SavedFilter{}, fmt.Errorf("create filter: %w", err)
	}
	return out, nil
}

// ListFilters returns the filters a person saved for a page, newest first.
func (s *Store) ListFilters(ctx context.Context, userID int64, page string) ([]model.SavedFilter, error) {
	rows, err := s.q.ListFilters(ctx, dbgen.ListFiltersParams{UserID: userID, Page: page})
	if err != nil {
		return nil, fmt.Errorf("list filters: %w", err)
	}
	out := make([]model.SavedFilter, 0, len(rows))
	for _, r := range rows {
		f, err := toFilter(r)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// DeleteFilter removes a filter that belongs to the person. Someone else's filter is
// ErrNotFound, the same as one that does not exist.
func (s *Store) DeleteFilter(ctx context.Context, userID, id int64) error {
	n, err := s.q.DeleteFilter(ctx, dbgen.DeleteFilterParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete filter: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
