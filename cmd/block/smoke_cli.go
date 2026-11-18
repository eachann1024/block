package main

import (
	"fmt"
	"os"

	"block/internal/smoke"
)

func runSmokeOnceCLI(dataDir string) int {
	baseDir := dataDir
	cleanup := false
	if baseDir == "" {
		tmp, err := os.MkdirTemp("", "block-go-smoke-cli-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SMOKE FAIL] mkdir temp: %v\n", err)
			return 1
		}
		baseDir = tmp
		cleanup = true
	}
	if cleanup {
		defer os.RemoveAll(baseDir)
	}

	rep, err := smoke.RunAll(baseDir)
	if rep != nil {
		for _, s := range rep.Steps {
			status := "PASS"
			if !s.Passed {
				status = "FAIL"
			}
			fmt.Printf("[%s] Step %02d: %s (%v) — %s\n", status, s.Index, s.Name, s.Duration, s.Detail)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[SMOKE FAIL] %v\n", err)
		return 1
	}
	fmt.Printf("[SMOKE PASS] %d/%d acceptance steps passed in %v\n", rep.PassedCount, len(rep.Steps), rep.TotalTime)
	return 0
}
