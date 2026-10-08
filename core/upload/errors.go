package upload

import "errors"

// Domain errors for the direct-upload (presigned) flow and size/quota
// validation. The HTTP layer maps each to a specific status code.
var (
	// ErrUnsupportedType: content type is not on the supported allow-list.
	ErrUnsupportedType = errors.New("unsupported file type")
	// ErrFileTooLarge: declared or actual size exceeds the per-file cap.
	ErrFileTooLarge = errors.New("file size exceeds the maximum upload size")
	// ErrQuotaExceeded: the user's storage quota would be exceeded.
	ErrQuotaExceeded = errors.New("storage quota exceeded")
	// ErrInvalidRequest: malformed begin payload (missing/invalid fields).
	ErrInvalidRequest = errors.New("invalid upload request")
	// ErrUploadNotStarted: complete was called before the object was PUT.
	ErrUploadNotStarted = errors.New("uploaded file not found in storage")
	// ErrSizeMismatch: the stored object is larger than declared at begin.
	ErrSizeMismatch = errors.New("uploaded size does not match the declared size")
	// ErrContentMismatch: the stored bytes are not media of the declared family.
	ErrContentMismatch = errors.New("uploaded file content does not match the declared type")
)
