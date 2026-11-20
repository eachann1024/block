package feedback

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/files"

	xdraw "golang.org/x/image/draw"
)

const (
	maxImageDecodeBytes = 64 << 20
	maxImagePixels      = 200_000_000
	maxImageEdgeLimit   = 100_000
	jpegEncodeQuality   = 85
	maxISOBMFFBoxes     = 4096
)

// Probe 检查用户选中的文件并准备好上传用的文件。
//   - path 不存在、是目录或读不了：返回 error。
//   - 按扩展名（不区分大小写）分类：图片 png/jpg/jpeg/gif/webp/bmp/heic/tiff；视频 mp4/mov/m4v/webm/mkv/avi；其余为 KindFile。
//   - 图片：仅 png/jpg/jpeg 尝试压缩（其余原样上传）。长边 > ImageMaxEdge 时等比缩小到 ImageMaxEdge（用 golang.org/x/image/draw 的 CatmullRom，已在 go.mod）；
//     JPEG 要按 EXIF 方向摆正；不含透明像素的重编码为 JPEG 质量 85，含透明的重编码为 PNG。
//     只有结果比原文件小才采用：写入临时目录，UploadPath 指向它，Temp=true，Name 的扩展名改成实际格式；否则 UploadPath=Path。
//     解码失败不算错误，原样上传。原文件 > 64 MB 的图片不解码，原样上传。
//   - 视频：解析 mp4/mov/m4v 的 moov→mvhd 得到时长（version 0 与 1 都要支持；moov 可能在文件尾，按 box 头跳读，不要整文件读进内存）；解析失败 Duration=0。
//   - ID：随机 8 字节 hex。Name = filepath.Base。OrigBytes/Bytes 填实际大小。
func Probe(path string) (Attachment, error) {
	if path == "" {
		return Attachment{}, errors.New("feedback: empty path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, err
	}
	if info.IsDir() {
		return Attachment{}, fmt.Errorf("feedback: %s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return Attachment{}, fmt.Errorf("feedback: %s is not a regular file", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return Attachment{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return Attachment{}, err
	}
	if !fi.Mode().IsRegular() {
		return Attachment{}, fmt.Errorf("feedback: %s is not a regular file", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	origBytes := fi.Size()
	att := Attachment{
		ID:         newAttachmentID(),
		Path:       path,
		UploadPath: path,
		Name:       filepath.Base(path),
		Kind:       classifyExt(ext),
		OrigBytes:  origBytes,
		Bytes:      origBytes,
	}

	switch att.Kind {
	case KindImage:
		if isCompressibleImageExt(ext) && origBytes > 0 && origBytes <= maxImageDecodeBytes {
			if compressed, ok := compressImageAttachment(f, att.Name, origBytes); ok {
				att.UploadPath = compressed.uploadPath
				att.Name = compressed.name
				att.Bytes = compressed.bytes
				att.Temp = true
			}
		}
	case KindVideo:
		if ext == ".mp4" || ext == ".mov" || ext == ".m4v" {
			att.Duration = probeISOBMFFDuration(f, origBytes)
		}
	}

	// Pin an immutable private snapshot in feedback-tmp for sendable files so
	// swapping the source path to a symlink/FIFO or editing/moving it before
	// Send can never upload unexpected data or stall os.Open.
	if !att.Temp && origBytes <= att.LimitBytes() {
		if snapPath, snapBytes, err := snapshotAttachment(f, ext, att.LimitBytes()); err == nil {
			att.UploadPath = snapPath
			att.OrigBytes = snapBytes
			att.Bytes = snapBytes
			att.Temp = true
		} else {
			return Attachment{}, err
		}
	}

	return att, nil
}

func snapshotAttachment(f *os.File, ext string, maxBytes int64) (string, int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	dir := tempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, err
	}
	tmpFile, err := os.CreateTemp(dir, "fb-snap-*"+safeTempExt(ext))
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Chmod(0o600)
	n, copyErr := io.Copy(tmpFile, io.LimitReader(f, maxBytes+1))
	closeErr := tmpFile.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, closeErr
	}
	return tmpPath, n, nil
}

func safeTempExt(ext string) string {
	if len(ext) < 2 || len(ext) > 10 || ext[0] != '.' {
		return ".bin"
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ".bin"
		}
	}
	return ext
}

// Discard 删除本附件拥有的临时文件（Temp 为 true 时）；可重复调用。
func (a Attachment) Discard() {
	if !a.Temp || a.UploadPath == "" || a.UploadPath == a.Path {
		return
	}
	_ = os.Remove(a.UploadPath)
}

// CleanTemp 删除临时目录里所有遗留文件（启动时调用）。
func CleanTemp() {
	_ = os.RemoveAll(tempDir())
}

func tempDir() string {
	return filepath.Join(files.AppDataDir(), "feedback-tmp")
}

func newAttachmentID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func classifyExt(ext string) Kind {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic", ".tiff":
		return KindImage
	case ".mp4", ".mov", ".m4v", ".webm", ".mkv", ".avi":
		return KindVideo
	default:
		return KindFile
	}
}

func isCompressibleImageExt(ext string) bool {
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg"
}

type compressedImageResult struct {
	uploadPath string
	name       string
	bytes      int64
}

func compressImageAttachment(r io.Reader, origName string, origBytes int64) (compressedImageResult, bool) {
	raw, err := io.ReadAll(io.LimitReader(r, maxImageDecodeBytes+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > maxImageDecodeBytes {
		return compressedImageResult{}, false
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > maxImageEdgeLimit || cfg.Height > maxImageEdgeLimit ||
		int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return compressedImageResult{}, false
	}

	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil || img == nil {
		return compressedImageResult{}, false
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return compressedImageResult{}, false
	}

	hasAlpha := imageHasAlpha(img)
	orient := 1
	if format == "jpeg" {
		orient = jpegExifOrientation(raw)
	}

	var outImg image.Image = img
	needsScale := w > ImageMaxEdge || h > ImageMaxEdge
	if needsScale {
		dstW, dstH := scaledDims(w, h, ImageMaxEdge)
		dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		if orient > 1 && orient <= 8 {
			dst = applyExifOrientation(dst, orient)
		}
		outImg = dst
	} else if orient > 1 && orient <= 8 {
		rgba := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.Copy(rgba, image.Point{}, img, b, xdraw.Src, nil)
		outImg = applyExifOrientation(rgba, orient)
	}

	var buf bytes.Buffer
	var outExt string
	if hasAlpha {
		outExt = ".png"
		if err := png.Encode(&buf, outImg); err != nil {
			return compressedImageResult{}, false
		}
	} else {
		outExt = ".jpg"
		if err := jpeg.Encode(&buf, outImg, &jpeg.Options{Quality: jpegEncodeQuality}); err != nil {
			return compressedImageResult{}, false
		}
	}

	encodedSize := int64(buf.Len())
	if encodedSize <= 0 || encodedSize >= origBytes {
		return compressedImageResult{}, false
	}

	dir := tempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return compressedImageResult{}, false
	}
	tmpFile, err := os.CreateTemp(dir, "fb-*"+outExt)
	if err != nil {
		return compressedImageResult{}, false
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Chmod(0o600)
	if _, err := tmpFile.Write(buf.Bytes()); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return compressedImageResult{}, false
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return compressedImageResult{}, false
	}

	return compressedImageResult{
		uploadPath: tmpPath,
		name:       replaceExt(origName, outExt),
		bytes:      encodedSize,
	}, true
}

func replaceExt(name, newExt string) string {
	oldExt := filepath.Ext(name)
	return strings.TrimSuffix(name, oldExt) + newExt
}

func scaledDims(w, h, maxEdge int) (int, int) {
	if w <= 0 || h <= 0 {
		return maxEdge, maxEdge
	}
	if w <= maxEdge && h <= maxEdge {
		return w, h
	}
	if w >= h {
		dstH := max(int(math.Round(float64(h)*float64(maxEdge)/float64(w))), 1)
		return maxEdge, dstH
	}
	dstW := max(int(math.Round(float64(w)*float64(maxEdge)/float64(h))), 1)
	return dstW, maxEdge
}

func imageHasAlpha(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return !o.Opaque()
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}

// jpegExifOrientation reads the EXIF Orientation tag (0x0112) from a JPEG APP1 segment.
func jpegExifOrientation(raw []byte) int {
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(raw) {
		if raw[i] != 0xFF {
			return 1
		}
		for i < len(raw) && raw[i] == 0xFF {
			i++
		}
		if i >= len(raw) {
			return 1
		}
		marker := raw[i]
		i++
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		if i+2 > len(raw) {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(raw[i : i+2]))
		if segLen < 2 || i+segLen > len(raw) {
			return 1
		}
		if marker == 0xE1 && segLen >= 16 && bytes.Equal(raw[i+2:i+8], []byte("Exif\x00\x00")) {
			return parseTiffOrientation(raw[i+8 : i+segLen])
		}
		i += segLen
	}
	return 1
}

func parseTiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	ifdOff := int(bo.Uint32(tiff[4:8]))
	if ifdOff < 8 || ifdOff+2 > len(tiff) {
		return 1
	}
	count := int(bo.Uint16(tiff[ifdOff : ifdOff+2]))
	for k := 0; k < count; k++ {
		off := ifdOff + 2 + k*12
		if off+12 > len(tiff) {
			break
		}
		if bo.Uint16(tiff[off:off+2]) == 0x0112 {
			return int(bo.Uint16(tiff[off+8 : off+10]))
		}
	}
	return 1
}

func applyExifOrientation(src *image.RGBA, orient int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orient >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orient {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			default:
				dx, dy = x, y
			}
			so := src.PixOffset(x, y)
			do := dst.PixOffset(dx, dy)
			copy(dst.Pix[do:do+4], src.Pix[so:so+4])
		}
	}
	return dst
}

func probeISOBMFFDuration(r io.ReaderAt, fileSize int64) time.Duration {
	if fileSize < 8 {
		return 0
	}
	pos := int64(0)
	for n := 0; n < maxISOBMFFBoxes && pos+8 <= fileSize; n++ {
		boxType, payloadStart, boxEnd, ok := readBoxHeader(r, pos, fileSize)
		if !ok {
			return 0
		}
		if boxType == "moov" {
			return probeMoovDuration(r, payloadStart, boxEnd)
		}
		if boxEnd <= pos {
			return 0
		}
		pos = boxEnd
	}
	return 0
}

func probeMoovDuration(r io.ReaderAt, start, end int64) time.Duration {
	pos := start
	for n := 0; n < maxISOBMFFBoxes && pos+8 <= end; n++ {
		boxType, payloadStart, boxEnd, ok := readBoxHeader(r, pos, end)
		if !ok {
			return 0
		}
		if boxType == "mvhd" {
			return parseMvhdDuration(r, payloadStart, boxEnd)
		}
		if boxEnd <= pos {
			return 0
		}
		pos = boxEnd
	}
	return 0
}

func readBoxHeader(r io.ReaderAt, pos, end int64) (boxType string, payloadStart, boxEnd int64, ok bool) {
	if pos < 0 || pos+8 > end {
		return "", 0, 0, false
	}
	var hdr [16]byte
	if !readExactAt(r, hdr[:8], pos) {
		return "", 0, 0, false
	}
	size32 := binary.BigEndian.Uint32(hdr[0:4])
	boxType = string(hdr[4:8])
	rem := uint64(end - pos)

	switch size32 {
	case 0:
		return boxType, pos + 8, end, true
	case 1:
		if pos+16 > end {
			return "", 0, 0, false
		}
		if !readExactAt(r, hdr[8:16], pos+8) {
			return "", 0, 0, false
		}
		size64 := binary.BigEndian.Uint64(hdr[8:16])
		if size64 < 16 || size64 > rem {
			return "", 0, 0, false
		}
		return boxType, pos + 16, pos + int64(size64), true
	default:
		size64 := uint64(size32)
		if size64 < 8 || size64 > rem {
			return "", 0, 0, false
		}
		return boxType, pos + 8, pos + int64(size64), true
	}
}

func parseMvhdDuration(r io.ReaderAt, start, end int64) time.Duration {
	payloadLen := end - start
	if payloadLen < 20 {
		return 0
	}
	var buf [32]byte
	want := min(int(payloadLen), len(buf))
	if !readExactAt(r, buf[:want], start) {
		return 0
	}

	version := buf[0]
	var timescale uint32
	var duration uint64

	switch version {
	case 0:
		if want < 20 {
			return 0
		}
		timescale = binary.BigEndian.Uint32(buf[12:16])
		duration = uint64(binary.BigEndian.Uint32(buf[16:20]))
	case 1:
		if want < 32 {
			return 0
		}
		timescale = binary.BigEndian.Uint32(buf[20:24])
		duration = binary.BigEndian.Uint64(buf[24:32])
	default:
		return 0
	}

	if timescale == 0 || duration == 0 {
		return 0
	}
	secs := duration / uint64(timescale)
	if secs > uint64(math.MaxInt64/int64(time.Second)) {
		return 0
	}
	remTicks := duration % uint64(timescale)
	ns := secs*uint64(time.Second) + (remTicks*uint64(time.Second))/uint64(timescale)
	if ns > uint64(math.MaxInt64) {
		return 0
	}
	return time.Duration(ns)
}

func readExactAt(r io.ReaderAt, dst []byte, off int64) bool {
	n, err := r.ReadAt(dst, off)
	if err != nil && (err != io.EOF || n < len(dst)) {
		return false
	}
	return n == len(dst)
}
