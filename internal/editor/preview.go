package editor

import (
	"net/url"

	"block/internal/model"
	appUi "block/internal/ui"
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
	session.embedded = true
	session.readOnly = true
	if media == nil {
		media = func(string, bool, bool) PreviewAsset { return PreviewAsset{Unavailable: true} }
	}
	session.previewMedia = media
	return session
}

// SetPreviewMediaHooks connects a read-only preview session (such as the App Store
// README view) to a host-owned media lightbox so clicking images/videos expands them
// and active video players are never mounted twice simultaneously.
func (s *Session) SetPreviewMediaHooks(
	onOpen func(src string, isVideo bool),
	isExpanded func(src string) (thisExpanded bool, anyLightboxOpen bool),
) {
	if s == nil {
		return
	}
	s.onPreviewMediaOpen = onOpen
	s.isPreviewMediaExpanded = isExpanded
}

// ResolvePreviewVideoPlayer returns (creating if loaded) the shared *ui.VideoPlayer
// for src so the inline preview and host lightbox reuse the same playback session.
func (s *Session) ResolvePreviewVideoPlayer(src string) (*ui.VideoPlayer, bool) {
	if s == nil {
		return nil, false
	}
	return s.resolveVideoPlayer(src)
}

// PauseAllVideos ends any active temporary 3x boost and pauses every loaded video player.
func (s *Session) PauseAllVideos() {
	if s == nil {
		return
	}
	for src, p := range s.videoPlayers {
		if p == nil {
			continue
		}
		if st := s.videoCtrlStates[src]; st != nil {
			st.EndPlayerBoost(p)
		}
		p.Pause()
	}
}

func (s *Session) previewExpandedState(src string) (bool, bool) {
	if s == nil || s.isPreviewMediaExpanded == nil {
		return false, false
	}
	return s.isPreviewMediaExpanded(src)
}

func (s *Session) videoCtrlState(src string) *appUi.VideoControlState {
	if s.videoCtrlStates == nil {
		s.videoCtrlStates = make(map[string]*appUi.VideoControlState)
	}
	st, ok := s.videoCtrlStates[src]
	if !ok {
		st = &appUi.VideoControlState{BaseRate: 1.0, SavedVolume: 1.0}
		s.videoCtrlStates[src] = st
	}
	return st
}

func (s *Session) ClosePreview() {
	s.closeVideoPlayers()
}

func IsPreviewVideo(src string) bool {
	u, err := url.Parse(src)
	return err == nil && IsSupportedVideoPath(u.Path)
}
