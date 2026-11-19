package media

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const (
	MaxImageBytes  = 20 << 20
	MaxImageSide   = 8192
	MaxImagePixels = 32_000_000

	MaxVideoBytes  = 128 << 20
	MaxImportBytes = 256 << 20
	MaxPasteBytes  = 5 << 20
)

func ImageConfig(raw []byte) (image.Config, string, error) {
	if len(raw) == 0 || len(raw) > MaxImageBytes {
		return image.Config{}, "", ErrInvalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !ImageSizeOK(cfg.Width, cfg.Height) || ImageMIME(format) == "" {
		return image.Config{}, "", ErrInvalid
	}
	return cfg, format, nil
}

func ImageSizeOK(w, h int) bool {
	return w > 0 && h > 0 && w <= MaxImageSide && h <= MaxImageSide && w*h <= MaxImagePixels
}

func DecodeRaster(raw []byte) (image.Image, image.Config, string, error) {
	cfg, format, err := ImageConfig(raw)
	if err != nil {
		return nil, image.Config{}, "", err
	}
	if format == "gif" {
		bitmap, err := decodeGIF(raw, cfg)
		if err != nil {
			return nil, image.Config{}, "", err
		}
		return bitmap, cfg, format, nil
	}
	bitmap, decodedFormat, err := image.Decode(bytes.NewReader(raw))
	if err != nil || decodedFormat != format || !ImageSizeOK(bitmap.Bounds().Dx(), bitmap.Bounds().Dy()) {
		return nil, image.Config{}, "", ErrInvalid
	}
	return bitmap, cfg, format, nil
}

func ImageMIME(format string) string {
	switch format {
	case "png", "jpeg", "gif", "webp", "bmp":
		return "image/" + format
	default:
		return ""
	}
}
