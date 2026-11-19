package markdown

import (
	"image"
	"os"
	"path/filepath"
	"sync"

	"block/internal/media"

	"github.com/egoist/mygo/ui"
)

const (
	maxExportImageRefs   = 128
	maxExportImageBytes  = 64 << 20
	maxExportImagePixels = 48_000_000
)

type exportImage struct {
	info   os.FileInfo
	raw    []byte
	cfg    image.Config
	format string
	cost   int

	bitmapOnce sync.Once
	bitmap     *ui.Bitmap
}

var exportImages struct {
	sync.Mutex
	entries map[string]*exportImage
	cost    int
}

func loadExportImage(notePath, src string, remainingBytes int) (*exportImage, bool) {
	if notePath == "" || remainingBytes <= 0 {
		return nil, false
	}
	dir, err := filepath.Abs(filepath.Dir(notePath))
	if err != nil {
		return nil, false
	}
	rel, err := media.RelativePath(dir, src)
	if err != nil {
		return nil, false
	}
	limit := int64(min(remainingBytes, media.MaxImageBytes))
	f, info, err := media.OpenLocal(dir, src, limit)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	key := filepath.Join(dir, rel)
	exportImages.Lock()
	entry := exportImages.entries[key]
	exportImages.Unlock()
	if entry != nil && entry.info.Size() == info.Size() && entry.info.ModTime().Equal(info.ModTime()) && os.SameFile(entry.info, info) {
		return entry, true
	}

	raw, err := media.ReadFile(f, limit)
	if err != nil {
		return nil, false
	}
	_, cfg, format, err := media.DecodeRaster(raw)
	if err != nil {
		return nil, false
	}
	entry = &exportImage{info: info, raw: raw, cfg: cfg, format: format, cost: len(raw) + 4*cfg.Width*cfg.Height}
	const cacheBytes = 128 << 20
	if entry.cost > cacheBytes {
		return entry, true
	}
	exportImages.Lock()
	if exportImages.entries == nil {
		exportImages.entries = make(map[string]*exportImage)
	}
	if old := exportImages.entries[key]; old != nil {
		exportImages.cost -= old.cost
	}
	if len(exportImages.entries) >= 64 || exportImages.cost+entry.cost > cacheBytes {
		clear(exportImages.entries)
		exportImages.cost = 0
	}
	exportImages.entries[key] = entry
	exportImages.cost += entry.cost
	exportImages.Unlock()
	return entry, true
}

func (e *exportImage) loadBitmap() *ui.Bitmap {
	e.bitmapOnce.Do(func() { e.bitmap, _ = ui.DecodeBitmap(e.raw) })
	return e.bitmap
}

func takeExportBitmap(notePath, src string, pixels *int) *ui.Bitmap {
	if *pixels <= 0 {
		return nil
	}
	entry, ok := loadExportImage(notePath, src, media.MaxImageBytes)
	if !ok {
		return nil
	}
	count := entry.cfg.Width * entry.cfg.Height
	if count > *pixels {
		return nil
	}
	*pixels -= count
	return entry.loadBitmap()
}
