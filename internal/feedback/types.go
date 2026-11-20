// Package feedback holds the data model, media preparation and upload
// transport behind the Feedback dialog: attachments (images, videos, files),
// their size limits, and the resumable per-part upload with retry.
package feedback

import (
	"fmt"
	"time"
)

// Kind classifies an attachment; it decides the size limit and the row icon.
type Kind int

const (
	KindFile Kind = iota
	KindImage
	KindVideo
)

// Limits. Images are measured after compression; everything else as-is.
const (
	MaxAttachments = 5
	MaxImageBytes  = 20 << 20
	MaxVideoBytes  = 50 << 20
	MaxFileBytes   = 20 << 20
	MaxTotalBytes  = 80 << 20
	// ImageMaxEdge is the long edge images are downscaled to before upload.
	ImageMaxEdge = 2560
)

// Attachment is one user-picked file, already probed and (for images)
// compressed. UploadPath is the file that is actually sent.
type Attachment struct {
	ID         string
	Path       string // the file the user picked
	UploadPath string // compressed temp file, or Path when sent as-is
	Name       string // base name shown in the row and sent to the server
	Kind       Kind
	OrigBytes  int64         // size of Path
	Bytes      int64         // size of UploadPath
	Duration   time.Duration // videos only; 0 when unknown
	Temp       bool          // UploadPath is a temp file owned by this attachment
}

// LimitBytes is the per-file size limit for this attachment's kind.
func (a Attachment) LimitBytes() int64 {
	switch a.Kind {
	case KindImage:
		return MaxImageBytes
	case KindVideo:
		return MaxVideoBytes
	default:
		return MaxFileBytes
	}
}

// OverLimit reports whether the attachment exceeds its per-file limit.
func (a Attachment) OverLimit() bool { return a.Bytes > a.LimitBytes() }

// Compressed reports whether the upload is smaller than the picked file.
func (a Attachment) Compressed() bool { return a.Temp && a.Bytes < a.OrigBytes }

// TotalBytes sums the upload size of a set of attachments.
func TotalBytes(list []Attachment) int64 {
	var n int64
	for _, a := range list {
		n += a.Bytes
	}
	return n
}

// FormatBytes renders a size the way the dialog shows it: "2 KB", "860 KB",
// "18.6 MB". Sizes of 10 MB and up drop the decimal ("212 MB").
func FormatBytes(n int64) string {
	const mb = 1 << 20
	switch {
	case n >= 10*mb:
		return fmt.Sprintf("%d MB", (n+mb/2)/mb)
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n <= 0:
		return "0 KB"
	default:
		return fmt.Sprintf("%d KB", max((n+512)/1024, 1))
	}
}

// FormatDuration renders a clip length as "0:24" or "12:05".
func FormatDuration(d time.Duration) string {
	secs := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}
