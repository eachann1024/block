package editor

import (
	"net/url"

	"block/internal/model"
	"github.com/egoist/mygo/ui"
)

// PreviewAsset is media supplied by a read-only document's resource loader.
type PreviewAsset struct {
	Image       ui.ImageSource
	Animation   *ui.VideoPlayer
	Video       []byte
	Loading     bool
	Unavailable bool
}

// NewReadOnlyPreview renders a document without workspace, file or link access.
// The caller owns media loading and passes load=false to inspect a video before playback is requested.
func NewReadOnlyPreview(doc *model.Doc, codec model.MarkdownCodec, media func(src string, video, load bool) PreviewAsset) *Session {
	session := NewSession("", doc, codec, nil)
	session.SetMinimal(true)
	session.readOnly = true
	if media == nil {
		media = func(string, bool, bool) PreviewAsset { return PreviewAsset{Unavailable: true} }
	}
	session.previewMedia = media
	return session
}

func (s *Session) ClosePreview() {
	s.closeVideoPlayers()
}

func IsPreviewVideo(src string) bool {
	u, err := url.Parse(src)
	return err == nil && IsSupportedVideoPath(u.Path)
}
