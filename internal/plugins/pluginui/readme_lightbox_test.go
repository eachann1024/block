package pluginui

import (
	"net/url"
	"testing"

	"block/internal/plugins"
)

func TestCollectReadmeMediaCollectsImagesAndVideosInOrder(t *testing.T) {
	md := `# Demo Plugin

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/banner-dark.png">
  <img alt="Hero Banner" src="assets/banner-light.png">
</picture>

Some description text.

![Settings Screenshot](https://example.com/screens/settings.png)

https://example.com/demos/walkthrough.mp4
`
	doc := parseReadmeBlocks(md, false)
	items := collectReadmeMedia(doc)
	if len(items) != 3 {
		t.Fatalf("expected 3 media items, got %d: %+v", len(items), items)
	}
	if items[0].src != "assets/banner-light.png" || items[0].alt != "Hero Banner" || items[0].isVideo {
		t.Fatalf("unexpected item[0]: %+v", items[0])
	}
	if items[1].src != "https://example.com/screens/settings.png" || items[1].alt != "Settings Screenshot" || items[1].isVideo {
		t.Fatalf("unexpected item[1]: %+v", items[1])
	}
	if items[2].src != "https://example.com/demos/walkthrough.mp4" || !items[2].isVideo {
		t.Fatalf("unexpected item[2]: %+v", items[2])
	}

	// Dark mode selects the dark <picture> source.
	darkDoc := parseReadmeBlocks(md, true)
	darkItems := collectReadmeMedia(darkDoc)
	if len(darkItems) != 3 || darkItems[0].src != "assets/banner-dark.png" {
		t.Fatalf("expected dark picture src, got %+v", darkItems)
	}
}

func TestStoreReadmeLightboxStateAndOpenURL(t *testing.T) {
	ctrl := NewController(nil)
	if ctrl.IsStoreLightboxOpen() {
		t.Fatalf("expected lightbox closed initially")
	}

	base, _ := url.Parse("https://raw.githubusercontent.com/blockmd-plugins/demo/main/README.md")
	preview := &storeReadmePreview{
		media: &readmeMedia{
			doc: plugins.PluginReadme{RemoteBase: base},
		},
		lightboxItems: []readmeMediaEntry{
			{src: "assets/banner.png", alt: "Banner", isVideo: false},
			{src: "https://cdn.example.com/demo.mp4", alt: "", isVideo: true},
		},
	}
	ctrl.mu.Lock()
	ctrl.storeReadme = preview
	ctrl.mu.Unlock()

	preview.openLightboxFor("https://cdn.example.com/demo.mp4", true)
	if !ctrl.IsStoreLightboxOpen() {
		t.Fatalf("expected IsStoreLightboxOpen() == true")
	}
	if preview.lightbox.Index != 1 {
		t.Fatalf("expected lightbox index 1, got %d", preview.lightbox.Index)
	}

	if got := preview.media.resolveOpenURL("assets/banner.png"); got != "https://raw.githubusercontent.com/blockmd-plugins/demo/main/assets/banner.png" {
		t.Fatalf("resolveOpenURL(relative) = %q", got)
	}
	if got := preview.media.resolveOpenURL("http://insecure.example.com/a.png"); got != "" {
		t.Fatalf("resolveOpenURL(http) should reject non-HTTPS, got %q", got)
	}
	if got := readmeMediaDisplayName(preview.lightboxItems[1]); got != "demo.mp4" {
		t.Fatalf("readmeMediaDisplayName(video) = %q, want demo.mp4", got)
	}
}
