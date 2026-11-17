package smoke

import "testing"

func TestFullWorkspaceClickAcceptance(t *testing.T) {
	rep, err := RunAll(t.TempDir())
	if err != nil {
		t.Fatalf("smoke acceptance failed: %v (passed=%d failed=%d)", err, rep.PassedCount, rep.FailedCount)
	}
	if rep.FailedCount != 0 || rep.PassedCount != 25 {
		t.Fatalf("unexpected step counts: passed=%d failed=%d", rep.PassedCount, rep.FailedCount)
	}
	for _, s := range rep.Steps {
		t.Logf("[PASS] Step %02d %-58s (%v) -> %s", s.Index, s.Name, s.Duration.Round(100_000), s.Detail)
	}
}
