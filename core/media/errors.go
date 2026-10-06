package media

import "errors"

var (
	// ErrNotFound is returned when media does not exist or the caller lacks access.
	// We use a single error to avoid leaking whether an ID exists (IDOR hygiene).
	ErrNotFound = errors.New("media not found")

	// ErrTrashed is returned when media is in the trash and therefore not
	// visible through regular read/serve routes. It maps to the same HTTP
	// response as ErrNotFound so callers cannot distinguish the two.
	ErrTrashed = errors.New("media is in trash")
)
