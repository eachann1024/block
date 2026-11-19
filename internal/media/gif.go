package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/gif"
)

func decodeGIF(raw []byte, cfg image.Config) (image.Image, error) {
	if !gifFramesFit(raw, cfg.Width, cfg.Height) {
		return nil, ErrInvalid
	}
	anim, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil || len(anim.Image) == 0 {
		return nil, ErrInvalid
	}
	return anim.Image[0], nil
}

// Inspect frame dimensions before DecodeAll allocates their pixel storage.
func gifFramesFit(raw []byte, w, h int) bool {
	if len(raw) < 13 {
		return false
	}
	start := 13 + gifColorTableSize(raw[10])
	if start >= len(raw) {
		return false
	}
	data := raw[start:]
	u16 := binary.LittleEndian.Uint16
	frames := 0
	pixels := 0
	for len(data) > 0 {
		switch data[0] {
		case 0x3b: // trailer
			return frames > 0 && len(data) == 1
		case 0x21: // extension with data sub-blocks
			if len(data) < 2 {
				return false
			}
			data = data[2:]
		case 0x2c: // image descriptor, optional color table, and LZW data
			if len(data) < 11 {
				return false
			}
			x, y := int(u16(data[1:])), int(u16(data[3:]))
			fw, fh := int(u16(data[5:])), int(u16(data[7:]))
			if fw == 0 || fh == 0 || x+fw > w || y+fh > h {
				return false
			}
			frames++
			pixels += w * h
			n := 11 + gifColorTableSize(data[9])
			if n > len(data) || frames > 120 || pixels > MaxImagePixels {
				return false
			}
			data = data[n:]
		default:
			return false
		}
		for {
			if len(data) == 0 || int(data[0]) >= len(data) {
				return false
			}
			n := int(data[0])
			data = data[n+1:]
			if n == 0 {
				break
			}
		}
	}
	return false
}

func gifColorTableSize(flags byte) int {
	if flags&0x80 == 0 {
		return 0
	}
	return 3 << (1 + (flags & 7))
}
