package model

import "errors"

// Errors shared by store, auth and api. store re-exports them under its own names.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("already exists")
	ErrLastAdmin = errors.New("cannot remove the last administrator")
)
