package smoke

import "testing"

func TestSceneShot(t *testing.T) {
	cfg, ok, err := ParseSceneShotEnv()
	if err != nil {
		t.Fatalf("ParseSceneShotEnv: %v", err)
	}
	if !ok {
		t.Skip("BLOCK_SHOT_SCENES not set; skipping offscreen scene snapshots")
	}

	summary, err := RunSceneShots(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("RunSceneShots: %v", err)
	}
	for _, shot := range summary.Shots {
		t.Logf("[SHOT] %-9s %-5s %-8s probes=%d anomalies=%d (%dms) -> %s",
			shot.Scene, shot.Theme, shot.Size, shot.ProbeCount, len(shot.Anomalies), shot.DurationMs, shot.ImagePath)
		for _, a := range shot.Anomalies {
			t.Logf("  [PROBE ANOMALY] id=%q rect=(%.1f, %.1f, %.1f, %.1f) reason=%s",
				a.ID, a.X, a.Y, a.W, a.H, a.Reason)
		}
	}
	t.Logf("[SUMMARY] shots=%d anomalies=%d out=%s", summary.TotalShots, summary.AnomalyCount, summary.OutputDir)
}
