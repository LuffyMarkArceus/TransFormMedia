package http

import "strings"

// normalizeContentType maps sniffed/browser MIME values to canonical types used by processors.
func normalizeContentType(mimeType string) string {
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))
	switch mimeType {
	case "image/jpg":
		return "image/jpeg"
	default:
		return mimeType
	}
}

func isSupportedMediaType(mimeType string) bool {
	mimeType = normalizeContentType(mimeType)
	supported := []string{
		"image/jpeg", "image/png",
		"video/mp4", "video/quicktime", "video/x-msvideo", "video/webm", "video/x-matroska",
		"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/ogg", "audio/flac", "audio/aac", "audio/mp4",
	}
	for _, t := range supported {
		if mimeType == t {
			return true
		}
	}
	return false
}
