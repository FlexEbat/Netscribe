package model

import "errors"

// Errors shared by store, auth and api. store re-exports them under its own names.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("already exists")
	ErrLastAdmin = errors.New("cannot remove the last administrator")

	ErrVersionConflict = errors.New("the record was changed by someone else")
	ErrNotManual       = errors.New("the link was found by a scan, not drawn by hand")
)
