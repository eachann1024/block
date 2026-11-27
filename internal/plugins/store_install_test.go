package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFirstInstallRequiresExplicitEnable(t *testing.T) {
	src := t.TempDir()
	m := Manifest{
		App: TargetApp, SchemaVersion: CurrentSchemaVersion, ID: "fixture-plugin",
		Name: LocalizedText{Zh: "测试插件", En: "Fixture plugin"}, Version: "1.0.0",
		Author: "Fixture", Summary: LocalizedText{Zh: "安装测试", En: "Install fixture"},
		Entry:    EntrySpec{JS: "fixture.js"},
		Surfaces: []Surface{SurfaceSettingsPage}, Icon: AppStoreIconSpec(),
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "fixture.js"), []byte("// installation validation only"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStoreManager(t.TempDir())
	installed, err := store.InstallFromDirectory(src)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Enabled || store.IsEnabled(m.ID) {
		t.Fatal("newly installed plugin was enabled")
	}
	if err := store.SetEnabled(m.ID, true); err != nil {
		t.Fatal(err)
	}
	if !store.IsEnabled(m.ID) {
		t.Fatal("explicit enable did not persist")
	}
}
