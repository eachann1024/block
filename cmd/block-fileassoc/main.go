package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"block/internal/plugins"
)

type output struct {
	MarkdownExtensions []string `json:"markdown_extensions"`
	DocumentExtensions []string `json:"document_extensions"`
	LinuxMIMETypes     []string `json:"linux_mime_types"`
}

func splitColonPaths(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ":") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func main() {
	installLinux := flag.Bool("install-linux-desktop", false, "Install Linux block.desktop with target-machine default-app protection")
	execPath := flag.String("exec", "", "Executable path for Linux desktop entry")
	dataHome := flag.String("data-home", "", "Target XDG_DATA_HOME directory")
	configHome := flag.String("config-home", "", "Target XDG_CONFIG_HOME directory")
	appData := flag.String("app-data", "", "Block state directory for file association metadata")
	dataDirs := flag.String("data-dirs", "", "Optional colon-separated XDG_DATA_DIRS override")
	configDirs := flag.String("config-dirs", "", "Optional colon-separated XDG_CONFIG_DIRS override")
	currentDesktop := flag.String("current-desktop", "", "Optional XDG_CURRENT_DESKTOP override")
	flag.Parse()

	if *installLinux {
		var resolvedDataDirs []string
		if *dataHome != "" {
			resolvedDataDirs = append(resolvedDataDirs, *dataHome)
		}
		if *dataDirs != "" {
			resolvedDataDirs = append(resolvedDataDirs, splitColonPaths(*dataDirs)...)
		} else if len(resolvedDataDirs) > 0 {
			// Let InstallLinuxDesktopEntry expand XDG_DATA_DIRS from environment.
			resolvedDataDirs = nil
		}
		var resolvedConfigDirs []string
		if *configHome != "" {
			resolvedConfigDirs = append(resolvedConfigDirs, *configHome)
		}
		if *configDirs != "" {
			resolvedConfigDirs = append(resolvedConfigDirs, splitColonPaths(*configDirs)...)
		} else if len(resolvedConfigDirs) > 0 {
			resolvedConfigDirs = nil
		}
		curDesktop := *currentDesktop
		if curDesktop == "" {
			curDesktop = os.Getenv("XDG_CURRENT_DESKTOP")
		}
		err := plugins.InstallLinuxDesktopEntry(plugins.LinuxDesktopInstallOptions{
			ExecPath:        *execPath,
			TargetDataDir:   *dataHome,
			TargetConfigDir: *configHome,
			StateDir:        *appData,
			DataDirs:        resolvedDataDirs,
			ConfigDirs:      resolvedConfigDirs,
			CurrentDesktop:  curDesktop,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	res := output{
		MarkdownExtensions: plugins.BuiltinMarkdownBareExtensions(),
		DocumentExtensions: plugins.BuiltinDocumentBareExtensions(),
		LinuxMIMETypes:     plugins.LinuxPackageMIMETypes(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
