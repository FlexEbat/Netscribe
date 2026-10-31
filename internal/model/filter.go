package model

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The pages that keep saved filters.
const (
	PageInventory  = "inventory"
	PageServices   = "services"
	PageContainers = "containers"
)

const (
	maxFilterNameRunes  = 40
	maxFilterQueryBytes = 300
	maxFilterValueRunes = 100
	// MaxFiltersPerPage is how many filters one person keeps for one page.
	MaxFiltersPerPage = 20
)

// The checks of a saved filter, in the words the API returns.
var (
	ErrFilterPage  = errors.New("page must be inventory, services or containers")
	ErrFilterName  = errors.New("name must be 1 to 40 characters without control characters")
	ErrFilterQuery = errors.New("query must be a query string of at most 300 characters")
	ErrFilterEmpty = errors.New("a filter must filter something")
)

// SavedFilter is a view that a person keeps for a page.
type SavedFilter struct {
	ID        int64     `json:"id"`
	Page      string    `json:"page"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"createdAt"`
}

// ValidFilterPage says whether a page keeps saved filters.
func ValidFilterPage(page string) bool {
	return page == PageInventory || page == PageServices || page == PageContainers
}

// CheckFilterName trims a filter name and checks it.
func CheckFilterName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxFilterNameRunes || hasUnsafe(name) {
		return "", ErrFilterName
	}
	return name, nil
}

var filterKeys = map[string]map[string]bool{
	PageInventory:  {"q": true, "status": true, "trust": true, "group": true},
	PageServices:   {"q": true},
	PageContainers: {"q": true},
}

// CheckFilterQuery checks the query of a filter for a page and returns its canonical form:
// keys in alphabetical order, empty values dropped. A key that the page does not know, a key
// that appears twice and a value that the page cannot use are refused.
func CheckFilterQuery(page, query string) (string, error) {
	if !ValidFilterPage(page) {
		return "", ErrFilterPage
	}
	if len(query) > maxFilterQueryBytes {
		return "", ErrFilterQuery
	}
	raw, err := url.ParseQuery(query)
	if err != nil {
		return "", ErrFilterQuery
	}
	clean := url.Values{}
	for key, values := range raw {
		if !filterKeys[page][key] {
			return "", errors.New("the filter uses " + strconv.Quote(key) + ", which the page does not have")
		}
		if len(values) != 1 {
			return "", errors.New("the filter names " + strconv.Quote(key) + " more than once")
		}
		v := strings.TrimSpace(values[0])
		if v == "" {
			continue
		}
		if err := checkFilterValue(key, v); err != nil {
			return "", err
		}
		clean.Set(key, v)
	}
	if len(clean) == 0 {
		return "", ErrFilterEmpty
	}
	return clean.Encode(), nil
}

func checkFilterValue(key, v string) error {
	switch key {
	case "q":
		if utf8.RuneCountInString(v) > maxFilterValueRunes || hasUnsafe(v) {
			return errors.New("q must be at most 100 characters without control characters")
		}
	case "status":
		if v != "online" && v != "offline" {
			return errors.New("status must be online or offline")
		}
	case "trust":
		if v != "unknown" && v != "trusted" {
			return errors.New("trust must be unknown or trusted")
		}
	case "group":
		if n, err := strconv.ParseInt(v, 10, 64); v != "none" && (err != nil || n < 1 || strconv.FormatInt(n, 10) != v) {
			return errors.New("group must be none or a group id")
		}
	}
	return nil
}
