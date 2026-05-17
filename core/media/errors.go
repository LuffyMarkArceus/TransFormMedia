package media

import "errors"

var (
	// ErrNotFound is returned when media does not exist or the caller lacks access.
	// We use a single error to avoid leaking whether an ID exists (IDOR hygiene).
	ErrNotFound = errors.New("media not found")
)
