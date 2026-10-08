package media

import "testing"

func TestDetectContentType(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", []byte{}, ""},
		{"garbage", []byte("hello world, not media at all"), ""},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}, "image/jpeg"},
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"mp4 isom", isobmff("isom", nil), "video/mp4"},
		{"mp4 mp42", isobmff("mp42", nil), "video/mp4"},
		{"mp4 compatible iso2", isobmff("avc1", []string{"iso2"}), "video/mp4"},
		{"quicktime", isobmff("qt  ", nil), "video/quicktime"},
		{"m4a", isobmff("M4A ", nil), "audio/mp4"},
		{"m4a compatible brand", isobmff("isom", []string{"M4A "}), "audio/mp4"},
		{"unknown ftyp brand", isobmff("heic", nil), ""},
		{"flac", []byte("fLaC\x00\x00\x00\x22"), "audio/flac"},
		{"mp3 id3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), "audio/mpeg"},
		{"mp3 frame sync no id3", []byte{0xFF, 0xFB, 0x90, 0x00}, "audio/mpeg"},
		{"aac adts", []byte{0xFF, 0xF1, 0x50, 0x80}, "audio/aac"},
		{"ogg", []byte("OggS\x00\x02\x00\x00"), "audio/ogg"},
		{"wav", append([]byte("RIFF\x24\x00\x00\x00WAVE"), 0), "audio/wav"},
		{"avi", append([]byte("RIFF\x24\x00\x00\x00AVI "), 0), "video/x-msvideo"},
		{"webm", append([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte("\x01\x00\x00\x00\x00\x00\x00\x1F\x42\x86\x81\x01\x42\xF7\x81\x01\x42\xF2\x81\x04\x42\xF3\x81\x08\x42\x82\x84webm")...), "video/webm"},
		{"matroska", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00}, "video/x-matroska"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectContentType(tc.data); got != tc.want {
				t.Fatalf("DetectContentType(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// isobmff builds a minimal ftyp box: size, "ftyp", major brand, minor
// version, then zero or more compatible brands.
func isobmff(major string, compatible []string) []byte {
	body := make([]byte, 0, 8+4*len(compatible))
	body = append(body, "ftyp"...)
	body = append(body, major...)
	body = append(body, 0, 0, 0, 0)
	for _, brand := range compatible {
		body = append(body, brand...)
	}
	size := len(body) + 4
	box := []byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
	return append(box, body...)
}
