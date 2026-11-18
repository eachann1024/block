package pluginui

import "encoding/binary"

// gifAnimationPixels bounds decoded frame storage before the toolkit's
// in-memory GIF decoder runs. Zero leaves the image as a static preview.
func gifAnimationPixels(raw []byte) int64 {
	if len(raw) < 13 || (string(raw[:6]) != "GIF87a" && string(raw[:6]) != "GIF89a") {
		return 0
	}
	u16 := binary.LittleEndian.Uint16
	w, h := int(u16(raw[6:])), int(u16(raw[8:]))
	if w == 0 || h == 0 || w > 2_000_000/h {
		return 0
	}
	tablesize := func(flags byte) int {
		if flags&0x80 == 0 {
			return 0
		}
		return 3 << (1 + (flags & 7))
	}
	data := raw[13:]
	if n := tablesize(raw[10]); n <= len(data) {
		data = data[n:]
	} else {
		return 0
	}
	frames, pixels := 0, int64(w)*int64(h)*3
	for len(data) > 0 {
		switch data[0] {
		case 0x3b:
			if frames > 1 {
				return pixels
			}
			return 0
		case 0x21:
			if len(data) < 2 {
				return 0
			}
			data = data[2:]
		case 0x2c:
			if len(data) < 11 {
				return 0
			}
			x, y := int(u16(data[1:])), int(u16(data[3:]))
			fw, fh := int(u16(data[5:])), int(u16(data[7:]))
			if fw == 0 || fh == 0 || x+fw > w || y+fh > h {
				return 0
			}
			frames++
			pixels += int64(fw) * int64(fh)
			n := 11 + tablesize(data[9])
			if n > len(data) || frames > 120 || pixels > readmePixelLimit {
				return 0
			}
			data = data[n:]
		default:
			return 0
		}
		for {
			if len(data) == 0 || int(data[0]) >= len(data) {
				return 0
			}
			n := int(data[0])
			data = data[n+1:]
			if n == 0 {
				break
			}
		}
	}
	return 0
}
