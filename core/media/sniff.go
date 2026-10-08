package media

import "bytes"

// DetectContentType identifies the canonical MIME type of the first bytes of
// a media file. It replaces net/http.DetectContentType for this service:
// the standard sniffer cannot identify several formats the API accepts
// (QuickTime, FLAC, AAC/ADTS, M4A, Ogg, ID3-less MP3, and WAV is reported
// as "audio/wave", which is not on the allow-list), so those uploads were
// rejected as unsupported. Returns "" when the bytes match no known format.
func DetectContentType(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	if bytes.Equal(data[:3], []byte{0xFF, 0xD8, 0xFF}) {
		return "image/jpeg"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}

	// Matroska / WebM share an EBML header; only the DocType string differs.
	if bytes.HasPrefix(data, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		if bytes.Contains(data[:min(len(data), 64)], []byte("webm")) {
			return "video/webm"
		}
		return "video/x-matroska"
	}

	// RIFF containers: AVI video and WAV audio.
	if bytes.HasPrefix(data, []byte("RIFF")) && len(data) >= 12 {
		switch string(data[8:12]) {
		case "AVI ":
			return "video/x-msvideo"
		case "WAVE":
			return "audio/wav"
		}
	}

	if bytes.HasPrefix(data, []byte("OggS")) {
		return "audio/ogg"
	}
	if bytes.HasPrefix(data, []byte("fLaC")) {
		return "audio/flac"
	}
	if ct := sniffISOBMFF(data); ct != "" {
		return ct
	}

	// MP3 with an ID3v2 tag.
	if bytes.HasPrefix(data, []byte("ID3")) {
		return "audio/mpeg"
	}
	// MPEG audio / AAC (ADTS) frame sync: 11 set bits. ADTS is the only
	// accepted format whose layer bits are 00.
	if data[0] == 0xFF && data[1]&0xE0 == 0xE0 {
		if data[1]&0x06 == 0x00 {
			return "audio/aac"
		}
		return "audio/mpeg"
	}

	return ""
}

// sniffISOBMFF recognizes size-prefixed "ftyp" boxes (MP4, QuickTime, M4A).
// Brands outside the known sets (e.g. HEIC/AVIF images, 3GP) return "" so
// they stay unsupported instead of being misclassified as video/mp4.
func sniffISOBMFF(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	boxSize := int(uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]))
	if boxSize%4 != 0 || boxSize < 12 || !bytes.Equal(data[4:8], []byte("ftyp")) {
		return ""
	}
	end := min(boxSize, len(data))

	major := data[8:12]
	if bytes.Equal(major, []byte("qt  ")) {
		return "video/quicktime"
	}
	if bytes.Equal(major, []byte("M4A ")) || bytes.Equal(major, []byte("M4B ")) {
		return "audio/mp4"
	}

	// Scan compatible brands (skipping the minor-version slot at offset 12).
	for st := 16; st+4 <= end; st += 4 {
		brand := data[st : st+4]
		if bytes.Equal(brand, []byte("qt  ")) {
			return "video/quicktime"
		}
		if bytes.Equal(brand, []byte("M4A ")) || bytes.Equal(brand, []byte("M4B ")) {
			return "audio/mp4"
		}
	}
	for st := 8; st+4 <= end; st += 4 {
		if st == 12 {
			continue
		}
		if isMP4Brand(data[st : st+4]) {
			return "video/mp4"
		}
	}
	return ""
}

func isMP4Brand(brand []byte) bool {
	for _, known := range [][]byte{
		[]byte("mp41"), []byte("mp42"), []byte("mp4v"),
		[]byte("isom"), []byte("iso2"), []byte("iso4"),
		[]byte("avc1"), []byte("mmp4"), []byte("dash"),
	} {
		if bytes.Equal(brand, known) {
			return true
		}
	}
	return false
}
