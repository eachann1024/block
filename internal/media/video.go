package media

import "bytes"

// IsVideo recognizes the file containers accepted for local media. The
// platform decoder still validates the container and its streams.
func IsVideo(raw []byte) bool {
	if len(raw) < 12 {
		return false
	}
	switch string(raw[4:8]) {
	case "ftyp", "moov", "mdat", "wide":
		return true // MP4 and QuickTime
	}
	switch {
	case bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("AVI ")):
		return true
	case bytes.Equal(raw[:4], []byte("OggS")):
		return true
	case bytes.Equal(raw[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return true // Matroska, WebM
	default:
		return false
	}
}
