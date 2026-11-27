package plugins

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"block/internal/files"
)

const (
	grantedPathsFile     = "granted-paths.json"
	pluginStateFile      = "state.json"
	maxGrantedPathsBytes = 512 * 1024
	maxGrantedPathsCount = 512
	maxFSReadDefault     = 16 * 1024 * 1024
	maxFSReadLimit       = 64 * 1024 * 1024
	maxFSWriteLimit      = 64 * 1024 * 1024
	maxFSListEntries     = 10000
)

// FSGrant describes a persisted filesystem path authorization for a plugin.
type FSGrant struct {
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	Source    string `json:"source,omitempty"`
	GrantedAt string `json:"granted_at,omitempty"`
}

var grantedPathsLocks sync.Map // cleaned DataDir -> *sync.Mutex

func grantedPathsLock(dataDir string) *sync.Mutex {
	mu, _ := grantedPathsLocks.LoadOrStore(filepath.Clean(dataDir), &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// GrantFSPath persists a directory or file authorization under apps-data/<id>/granted-paths.json.
func GrantFSPath(dataDir, path, source string) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("plugin data directory is required")
	}
	canon, err := resolveCanonicalPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(canon)
	isDir := err == nil && info.IsDir()

	mu := grantedPathsLock(dataDir)
	mu.Lock()
	defer mu.Unlock()

	root, err := openPluginDataRoot(dataDir)
	if err != nil {
		return err
	}
	defer root.Close()

	grants, err := readFSGrantsFromRoot(root)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	updated := false
	for i := range grants {
		if grants[i].Path == canon {
			grants[i].IsDir = isDir
			if source != "" {
				grants[i].Source = source
			}
			grants[i].GrantedAt = now
			updated = true
			break
		}
	}
	if !updated {
		if len(grants) >= maxGrantedPathsCount {
			grants = append([]FSGrant(nil), grants[len(grants)-maxGrantedPathsCount+1:]...)
		}
		grants = append(grants, FSGrant{
			Path:      canon,
			IsDir:     isDir,
			Source:    source,
			GrantedAt: now,
		})
	}
	raw, err := json.MarshalIndent(grants, "", "  ")
	if err != nil {
		return err
	}
	return files.WriteNoteAt(root, grantedPathsFile, string(raw))
}

// ListFSGrants returns persisted filesystem authorizations for a plugin data directory.
func ListFSGrants(dataDir string) ([]FSGrant, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("plugin data directory is required")
	}
	mu := grantedPathsLock(dataDir)
	mu.Lock()
	defer mu.Unlock()

	if _, err := os.Stat(dataDir); errors.Is(err, os.ErrNotExist) {
		return []FSGrant{}, nil
	}
	root, err := openPluginDataRoot(dataDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readFSGrantsFromRoot(root)
}

func readFSGrantsFromRoot(root *os.Root) ([]FSGrant, error) {
	b, err := readPluginFile(root, grantedPathsFile, maxGrantedPathsBytes)
	if errors.Is(err, os.ErrNotExist) {
		return []FSGrant{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return []FSGrant{}, nil
	}
	var grants []FSGrant
	if err := json.Unmarshal(b, &grants); err != nil {
		return nil, err
	}
	if grants == nil {
		return []FSGrant{}, nil
	}
	return grants, nil
}

func resolveCanonicalPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || strings.ContainsRune(raw, 0) || !utf8.ValidString(raw) {
		return "", errors.New("invalid path")
	}
	if !filepath.IsAbs(raw) {
		return "", errors.New("path must be absolute")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	var tail []string
	cur := abs
	for {
		_, lerr := os.Lstat(cur)
		if lerr == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(lerr, os.ErrNotExist) {
			return "", lerr
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func resolveCanonicalLeafPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || strings.ContainsRune(raw, 0) || !utf8.ValidString(raw) {
		return "", errors.New("invalid path")
	}
	if !filepath.IsAbs(raw) {
		return "", errors.New("path must be absolute")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return resolveCanonicalPath(raw)
	}
	canonParent, err := resolveCanonicalPath(parent)
	if err != nil {
		return "", err
	}
	base := filepath.Base(abs)
	if base == "" || base == "." || base == ".." {
		return canonParent, nil
	}
	return filepath.Join(canonParent, base), nil
}

type fsScopeEntry struct {
	canonPath string
	isDir     bool
	writable  bool
	isDataDir bool
	source    string
}

type resolvedFSTarget struct {
	canonPath string
	rootDir   string
	rel       string
	isDirRoot bool
	isDataDir bool
	writable  bool
}

func (rt *Runtime) resolveDownloadsDir() string {
	rt.cfgMu.RLock()
	d := strings.TrimSpace(rt.cfg.DownloadsDir)
	rt.cfgMu.RUnlock()
	if d == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			d = filepath.Join(home, "Downloads")
		}
	}
	if d == "" {
		return ""
	}
	canon, err := resolveCanonicalPath(d)
	if err != nil {
		return ""
	}
	return canon
}

func (rt *Runtime) collectFSScopes() ([]fsScopeEntry, error) {
	scopes := make([]fsScopeEntry, 0, 8)
	if strings.TrimSpace(rt.cfg.DataDir) != "" {
		if canon, err := resolveCanonicalPath(rt.cfg.DataDir); err == nil {
			scopes = append(scopes, fsScopeEntry{
				canonPath: canon,
				isDir:     true,
				writable:  true,
				isDataDir: true,
				source:    "data_dir",
			})
		}
	}
	if dl := rt.resolveDownloadsDir(); dl != "" {
		scopes = append(scopes, fsScopeEntry{
			canonPath: dl,
			isDir:     true,
			writable:  true,
			source:    "downloads",
		})
	}
	hasNotesRead := rt.manifest.HasPermission(PermNotesRead)
	hasNotesWrite := rt.manifest.HasPermission(PermNotesWrite)
	if hasNotesRead || hasNotesWrite {
		for _, nb := range rt.knownNotebooks() {
			if canon, err := resolveCanonicalPath(nb); err == nil {
				scopes = append(scopes, fsScopeEntry{
					canonPath: canon,
					isDir:     true,
					writable:  hasNotesWrite,
					source:    "notebook",
				})
			}
		}
	}
	if strings.TrimSpace(rt.cfg.DataDir) != "" {
		grants, err := ListFSGrants(rt.cfg.DataDir)
		if err != nil {
			return nil, err
		}
		for _, g := range grants {
			canon, err := resolveCanonicalPath(g.Path)
			if err != nil {
				continue
			}
			isDir := g.IsDir
			if info, err := os.Stat(canon); err == nil {
				isDir = info.IsDir()
			}
			scopes = append(scopes, fsScopeEntry{
				canonPath: canon,
				isDir:     isDir,
				writable:  true,
				source:    g.Source,
			})
		}
	}
	return scopes, nil
}

func pathWithinDir(target, dir string) (string, bool) {
	if target == dir {
		return ".", true
	}
	if dir == string(filepath.Separator) {
		rel, err := filepath.Rel(dir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}
	prefix := dir + string(filepath.Separator)
	if !strings.HasPrefix(target, prefix) {
		return "", false
	}
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

type protectedDataFile struct {
	name string
	info os.FileInfo
}

func (rt *Runtime) ensureDataRoot() (*os.Root, string, string, *rpcError) {
	rt.cfgMu.RLock()
	cfgDataDir := strings.TrimSpace(rt.cfg.DataDir)
	root := rt.dataRoot
	info := rt.dataRootInfo
	dataAbs := rt.dataRootAbs
	dataCanon := rt.dataRootCanon
	rt.cfgMu.RUnlock()

	if cfgDataDir == "" {
		return nil, "", "", rpcErr(-32004, "plugin data directory unavailable")
	}
	if root != nil && info != nil && dataAbs != "" && dataCanon != "" {
		return root, dataAbs, dataCanon, nil
	}

	rt.cfgMu.Lock()
	defer rt.cfgMu.Unlock()
	if rt.dataRoot == nil {
		opened, err := openPluginDataRoot(cfgDataDir)
		if err != nil {
			return nil, "", "", rpcErr(-32004, err.Error())
		}
		rt.dataRoot = opened
	}
	if rt.dataRootInfo == nil {
		if st, err := rt.dataRoot.Stat("."); err == nil {
			rt.dataRootInfo = st
		}
	}
	if rt.dataRootAbs == "" {
		if abs, err := filepath.Abs(cfgDataDir); err == nil {
			rt.dataRootAbs = filepath.Clean(abs)
		}
	}
	if rt.dataRootCanon == "" {
		if canon, err := resolveCanonicalPath(cfgDataDir); err == nil && canon != "" {
			rt.dataRootCanon = canon
		} else {
			rt.dataRootCanon = rt.dataRootAbs
		}
	}
	return rt.dataRoot, rt.dataRootAbs, rt.dataRootCanon, nil
}

func (rt *Runtime) verifyDataRootStartupIdentity(dataRoot *os.Root) *rpcError {
	rt.cfgMu.RLock()
	cfgDataDir := strings.TrimSpace(rt.cfg.DataDir)
	startupInfo := rt.dataRootInfo
	rt.cfgMu.RUnlock()
	if cfgDataDir == "" || dataRoot == nil || startupInfo == nil {
		return rpcErr(-32004, "plugin data directory unavailable")
	}
	dirAbs, err := filepath.Abs(cfgDataDir)
	if err != nil {
		return rpcErr(-32004, err.Error())
	}
	curInfo, err := os.Lstat(filepath.Clean(dirAbs))
	if err != nil {
		return rpcErr(-32004, "plugin data directory unavailable: "+err.Error())
	}
	if files.IsSymlinkOrReparseInfo(curInfo) || !curInfo.IsDir() {
		return rpcErr(-32004, "plugin data directory changed or replaced by symlink")
	}
	handleInfo, err := dataRoot.Stat(".")
	if err != nil {
		return rpcErr(-32004, err.Error())
	}
	if !os.SameFile(curInfo, startupInfo) || !os.SameFile(handleInfo, startupInfo) || !os.SameFile(curInfo, handleInfo) {
		return rpcErr(-32004, "plugin data directory moved or changed since startup")
	}
	return nil
}

func collectProtectedDataFiles(dataRoot *os.Root) []protectedDataFile {
	if dataRoot == nil {
		return nil
	}
	var protected []protectedDataFile
	for _, reserved := range []string{grantedPathsFile, schedulerFile, pluginStateFile} {
		if info, err := dataRoot.Lstat(reserved); err == nil {
			protected = append(protected, protectedDataFile{name: reserved, info: info})
		}
	}
	return protected
}

func isProtectedDataTarget(protected []protectedDataFile, rel string, info os.FileInfo) (string, bool) {
	if reserved, blocked := matchProtectedDataFile(rel); blocked {
		return reserved, true
	}
	if info != nil {
		for _, p := range protected {
			if p.info != nil && os.SameFile(info, p.info) {
				return p.name, true
			}
		}
	}
	return "", false
}

func verifyFSRootIdentity(root *os.Root, target *resolvedFSTarget, expectedRootID string) *rpcError {
	if expectedRootID == "" {
		return nil
	}
	if target == nil || !target.isDirRoot {
		return rpcErr(errCodeNotebookRootMismatch, ErrNotebookRootMismatch.Error())
	}
	return verifyRootExpectedIdentity(root, expectedRootID)
}

func (rt *Runtime) findDeepestNotebookRoot(canonPath string) (string, string, bool) {
	bestRoot := ""
	bestRel := ""
	for _, nb := range rt.knownNotebooks() {
		canonNb, err := resolveCanonicalLeafPath(nb)
		if err != nil || canonNb == "" {
			continue
		}
		rel, ok := pathWithinDir(canonPath, canonNb)
		if !ok {
			continue
		}
		if len(canonNb) > len(bestRoot) {
			bestRoot = canonNb
			bestRel = rel
		}
	}
	return bestRoot, bestRel, bestRoot != ""
}

func openVerifiedDirRoot(dirPath, expectedRootID string) (*os.Root, *rpcError) {
	linfo, lerr := os.Lstat(dirPath)
	if lerr != nil || files.IsSymlinkOrReparseInfo(linfo) || !linfo.IsDir() {
		return nil, rpcErr(errCodeNotebookRootMismatch, ErrNotebookRootMismatch.Error())
	}
	root, err := os.OpenRoot(dirPath)
	if err != nil {
		return nil, rpcErr(errCodeNotebookRootMismatch, ErrNotebookRootMismatch.Error())
	}
	hinfo, herr := root.Stat(".")
	if herr != nil || files.IsSymlinkOrReparseInfo(hinfo) || !hinfo.IsDir() || !os.SameFile(linfo, hinfo) {
		_ = root.Close()
		return nil, rpcErr(errCodeNotebookRootMismatch, ErrNotebookRootMismatch.Error())
	}
	if rerr := verifyRootExpectedIdentity(root, expectedRootID); rerr != nil {
		_ = root.Close()
		return nil, rerr
	}
	return root, nil
}

// openVerifiedFSOperationRoot opens the single os.Root handle under which a host.fs mutation
// executes and verifies expected_root_identity against it. When expected_root is provided,
// expected_root_identity is mandatory and targets must reside inside expected_root (-32602).
// When only expected_root_identity is set (legacy callers), registered notebook roots take
// precedence over overlapping directory grants such as Downloads.
func (rt *Runtime) openVerifiedFSOperationRoot(expectedRootRaw, expectedRootIDRaw string, primary *resolvedFSTarget, extra ...*resolvedFSTarget) (*os.Root, func(), *rpcError) {
	if expectedRootRaw != "" && strings.TrimSpace(expectedRootRaw) == "" {
		return nil, nil, rpcErr(-32602, "invalid expected_root")
	}
	hasExpRoot := expectedRootRaw != ""
	if hasExpRoot && expectedRootIDRaw == "" {
		return nil, nil, rpcErr(-32602, "expected_root requires expected_root_identity")
	}
	expectedRootID, rerr := parseExpectedRootIdentityParam(expectedRootIDRaw)
	if rerr != nil {
		return nil, nil, rerr
	}

	targets := make([]*resolvedFSTarget, 0, 1+len(extra))
	if primary != nil {
		targets = append(targets, primary)
	}
	for _, tgt := range extra {
		if tgt != nil {
			targets = append(targets, tgt)
		}
	}

	if hasExpRoot {
		expTarget, rerr := rt.resolveFSScopeOpts(expectedRootRaw, true, false)
		if rerr != nil {
			return nil, nil, rerr
		}
		if !expTarget.isDirRoot {
			return nil, nil, rpcErr(-32602, "expected_root must be inside an authorized directory grant")
		}
		expRoot, rerr := openVerifiedDirRoot(expTarget.canonPath, expectedRootID)
		if rerr != nil {
			return nil, nil, rerr
		}
		cleanup := func() { _ = expRoot.Close() }
		for _, tgt := range targets {
			rel, ok := pathWithinDir(tgt.canonPath, expTarget.canonPath)
			if !ok {
				cleanup()
				return nil, nil, rpcErr(-32602, "target path is outside expected_root")
			}
			if rel == "." {
				cleanup()
				return nil, nil, rpcErr(-32602, "target path cannot be expected_root itself")
			}
			tgt.rootDir = expTarget.canonPath
			tgt.rel = rel
			tgt.isDirRoot = true
		}
		return expRoot, cleanup, nil
	}

	if expectedRootID != "" && primary != nil {
		if nbRootPath, rel, ok := rt.findDeepestNotebookRoot(primary.canonPath); ok {
			if rel == "." {
				return nil, nil, rpcErr(-32602, "cannot modify notebook root directory itself")
			}
			nbRoot, rerr := openVerifiedDirRoot(nbRootPath, expectedRootID)
			if rerr != nil {
				return nil, nil, rerr
			}
			cleanup := func() { _ = nbRoot.Close() }
			for _, tgt := range extra {
				if subRel, subOK := pathWithinDir(tgt.canonPath, nbRootPath); subOK && subRel != "." {
					tgt.rootDir = nbRootPath
					tgt.rel = subRel
					tgt.isDirRoot = true
				} else if tgt != nil {
					cleanup()
					return nil, nil, rpcErr(-32602, "target path is outside verified notebook root")
				}
			}
			primary.rootDir = nbRootPath
			primary.rel = rel
			primary.isDirRoot = true
			return nbRoot, cleanup, nil
		}
	}

	root, err := os.OpenRoot(primary.rootDir)
	if err != nil {
		return nil, nil, rpcErr(-32004, err.Error())
	}
	cleanup := func() { _ = root.Close() }
	if rerr := verifyFSRootIdentity(root, primary, expectedRootID); rerr != nil {
		cleanup()
		return nil, nil, rerr
	}
	return root, cleanup, nil
}

func matchProtectedDataFile(rel string) (string, bool) {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		trimmed := strings.TrimRight(seg, ". ")
		for _, reserved := range []string{grantedPathsFile, schedulerFile, pluginStateFile} {
			if strings.EqualFold(trimmed, reserved) {
				return reserved, true
			}
		}
	}
	return "", false
}

func (rt *Runtime) resolveFSScope(rawPath string, write bool) (*resolvedFSTarget, *rpcError) {
	return rt.resolveFSScopeOpts(rawPath, write, true)
}

func (rt *Runtime) resolveFSScopeOpts(rawPath string, write bool, followLeafSymlink bool) (*resolvedFSTarget, *rpcError) {
	var (
		canon string
		err   error
	)
	if followLeafSymlink {
		canon, err = resolveCanonicalPath(rawPath)
	} else {
		canon, err = resolveCanonicalLeafPath(rawPath)
	}
	if err != nil {
		return nil, rpcErr(-32602, err.Error())
	}
	scopes, err := rt.collectFSScopes()
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}

	var dataRoot *os.Root
	var protectedData []protectedDataFile
	checkDataProtection := func(relInData string) *rpcError {
		if dataRoot != nil {
			var info os.FileInfo
			if relInData != "" && relInData != "." {
				if st, err := dataRoot.Lstat(relInData); err == nil {
					info = st
				}
			}
			if reserved, blocked := isProtectedDataTarget(protectedData, relInData, info); blocked {
				return rpcErr(-32001, "cannot modify "+reserved+" directly")
			}
			return nil
		}
		if reserved, blocked := matchProtectedDataFile(relInData); blocked {
			return rpcErr(-32001, "cannot modify "+reserved+" directly")
		}
		return nil
	}

	if write && strings.TrimSpace(rt.cfg.DataDir) != "" {
		if dr, _, dataCanon, dErr := rt.ensureDataRoot(); dErr == nil && dr != nil {
			dataRoot = dr
			protectedData = collectProtectedDataFiles(dataRoot)
			if relInData, ok := pathWithinDir(canon, dataCanon); ok {
				if rerr := checkDataProtection(relInData); rerr != nil {
					return nil, rerr
				}
			}
		}
	}

	var matchedReadOnly bool
	for _, sc := range scopes {
		var rel string
		var rootDir string
		matched := false
		if sc.isDir {
			if r, ok := pathWithinDir(canon, sc.canonPath); ok {
				matched = true
				rel = r
				rootDir = sc.canonPath
			}
		} else if canon == sc.canonPath {
			matched = true
			rel = filepath.Base(canon)
			rootDir = filepath.Dir(canon)
		}
		if !matched {
			continue
		}
		if write && !sc.writable {
			matchedReadOnly = true
			continue
		}
		if write && sc.isDataDir {
			if rerr := checkDataProtection(rel); rerr != nil {
				return nil, rerr
			}
		}
		return &resolvedFSTarget{
			canonPath: canon,
			rootDir:   rootDir,
			rel:       rel,
			isDirRoot: sc.isDir,
			isDataDir: sc.isDataDir,
			writable:  sc.writable,
		}, nil
	}
	if matchedReadOnly {
		return nil, rpcErr(-32001, "path is read-only without notes:write permission")
	}
	return nil, rpcErr(-32001, "path is outside authorized filesystem scope")
}

func isICloudPlaceholder(name string, info os.FileInfo) bool {
	if strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".icloud") && len(name) > len("..icloud") {
		return true
	}
	return isPlatformICloudPlaceholder(info)
}

func fsInfoEntry(fullPath, name string, info os.FileInfo) map[string]any {
	isSymlink := info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
	isDir := info.IsDir() && !isSymlink
	isRegular := info.Mode().IsRegular() && !isSymlink
	icloud := false
	if !isSymlink {
		icloud = isICloudPlaceholder(name, info)
	}
	return map[string]any{
		"exists":                true,
		"path":                  fullPath,
		"name":                  name,
		"is_dir":                isDir,
		"is_regular":            isRegular,
		"is_symlink":            isSymlink,
		"size":                  info.Size(),
		"mod_time":              info.ModTime().UTC().Format(time.RFC3339Nano),
		"mod_time_ms":           info.ModTime().UnixMilli(),
		"is_icloud_placeholder": icloud,
	}
}

func (rt *Runtime) dispatchFSHostMethod(method string, params json.RawMessage) (any, *rpcError) {
	if !rt.manifest.HasPermission(PermSystemFS) {
		return nil, missingPerm(PermSystemFS)
	}
	switch method {
	case "host.fs.list_grants":
		return rt.fsListGrants()
	case "host.fs.stat":
		return rt.fsStat(params)
	case "host.fs.list":
		return rt.fsList(params)
	case "host.fs.read":
		return rt.fsRead(params)
	case "host.fs.write":
		return rt.fsWrite(params)
	case "host.fs.mkdir":
		return rt.fsMkdir(params)
	case "host.fs.delete":
		return rt.fsDelete(params)
	case "host.fs.delete_data":
		return rt.fsDeleteData(params)
	case "host.fs.rename":
		return rt.fsRename(params)
	case "host.fs.copy":
		return rt.fsCopy(params)
	case "host.fs.atomic_swap":
		return rt.fsAtomicSwap(params)
	default:
		return nil, rpcErr(-32601, "unknown host method: "+method)
	}
}

func (rt *Runtime) fsListGrants() (any, *rpcError) {
	grants, err := ListFSGrants(rt.cfg.DataDir)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	dataDir := ""
	if strings.TrimSpace(rt.cfg.DataDir) != "" {
		if canon, err := resolveCanonicalPath(rt.cfg.DataDir); err == nil {
			dataDir = canon
		}
	}
	notebooks := make([]map[string]any, 0)
	hasRead := rt.manifest.HasPermission(PermNotesRead)
	hasWrite := rt.manifest.HasPermission(PermNotesWrite)
	if hasRead || hasWrite {
		for _, nb := range rt.knownNotebooks() {
			if canon, err := resolveCanonicalPath(nb); err == nil {
				notebooks = append(notebooks, map[string]any{
					"path":     canon,
					"writable": hasWrite,
				})
			}
		}
	}
	return map[string]any{
		"grants":        grants,
		"data_dir":      dataDir,
		"downloads_dir": rt.resolveDownloadsDir(),
		"notebooks":     notebooks,
	}, nil
}

func (rt *Runtime) fsStat(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path  string `json:"path"`
		Lstat bool   `json:"lstat"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.stat requires non-empty path")
	}
	target, rerr := rt.resolveFSScopeOpts(p.Path, false, !p.Lstat)
	if rerr != nil {
		return nil, rerr
	}
	root, err := os.OpenRoot(target.rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"exists": false, "path": target.canonPath}, nil
		}
		return nil, rpcErr(-32004, err.Error())
	}
	defer root.Close()
	var info os.FileInfo
	if p.Lstat {
		info, err = root.Lstat(target.rel)
	} else {
		info, err = root.Stat(target.rel)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"exists": false, "path": target.canonPath}, nil
		}
		return nil, rpcErr(-32004, err.Error())
	}
	name := info.Name()
	if target.rel == "." {
		name = filepath.Base(target.canonPath)
	}
	entry := fsInfoEntry(target.canonPath, name, info)
	if isDir, _ := entry["is_dir"].(bool); isDir {
		if target.rel == "." {
			if rid, ok := files.RootIdentityOfRoot(root); ok {
				entry["root_identity"] = rid
			}
		} else if subRoot, subErr := root.OpenRoot(target.rel); subErr == nil {
			if subInfo, stErr := subRoot.Stat("."); stErr == nil && os.SameFile(info, subInfo) {
				if rid, ok := files.RootIdentityOfRoot(subRoot); ok {
					entry["root_identity"] = rid
				}
			}
			_ = subRoot.Close()
		}
	}
	return entry, nil
}

func (rt *Runtime) fsList(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.list requires non-empty path")
	}
	target, rerr := rt.resolveFSScope(p.Path, false)
	if rerr != nil {
		return nil, rerr
	}
	if !target.isDirRoot {
		return nil, rpcErr(-32602, "host.fs.list requires a directory path")
	}
	root, err := os.OpenRoot(target.rootDir)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	defer root.Close()

	info, err := root.Stat(target.rel)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	if !info.IsDir() {
		return nil, rpcErr(-32602, "target is not a directory")
	}

	entries := make([]map[string]any, 0)
	truncated := false
	if !p.Recursive {
		dirEntries, err := fs.ReadDir(root.FS(), target.rel)
		if err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
		for _, de := range dirEntries {
			if len(entries) >= maxFSListEntries {
				truncated = true
				break
			}
			di, err := de.Info()
			if err != nil {
				continue
			}
			full := filepath.Join(target.canonPath, de.Name())
			entries = append(entries, fsInfoEntry(full, de.Name(), di))
		}
	} else {
		err := fs.WalkDir(root.FS(), target.rel, func(walkPath string, de fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if walkPath == target.rel {
				return nil
			}
			if len(entries) >= maxFSListEntries {
				truncated = true
				return fs.SkipAll
			}
			di, err := de.Info()
			if err != nil {
				return nil
			}
			relFromTarget := walkPath
			if target.rel != "." {
				relFromTarget = strings.TrimPrefix(walkPath, target.rel+"/")
			}
			full := filepath.Join(target.canonPath, filepath.FromSlash(relFromTarget))
			item := fsInfoEntry(full, de.Name(), di)
			item["rel_path"] = relFromTarget
			entries = append(entries, item)
			return nil
		})
		if err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i]["path"].(string) < entries[j]["path"].(string)
	})
	return map[string]any{
		"path":      target.canonPath,
		"entries":   entries,
		"truncated": truncated,
	}, nil
}

func (rt *Runtime) fsRead(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path     string `json:"path"`
		Encoding string `json:"encoding"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.read requires non-empty path")
	}
	enc := strings.ToLower(strings.TrimSpace(p.Encoding))
	if enc == "" {
		enc = "utf8"
	}
	if enc != "utf8" && enc != "base64" {
		return nil, rpcErr(-32602, "encoding must be \"utf8\" or \"base64\"")
	}
	limit := p.MaxBytes
	if limit < 0 {
		return nil, rpcErr(-32602, "max_bytes must be non-negative")
	}
	if limit == 0 {
		limit = maxFSReadDefault
	}
	if limit > maxFSReadLimit {
		limit = maxFSReadLimit
	}

	target, rerr := rt.resolveFSScope(p.Path, false)
	if rerr != nil {
		return nil, rerr
	}
	root, err := os.OpenRoot(target.rootDir)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	defer root.Close()

	data, err := readPluginFile(root, target.rel, limit)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	if enc == "base64" {
		return map[string]any{
			"path":           target.canonPath,
			"encoding":       "base64",
			"content_base64": base64.StdEncoding.EncodeToString(data),
			"size":           len(data),
		}, nil
	}
	if !utf8.Valid(data) {
		return nil, rpcErr(-32004, "file is not valid UTF-8; use encoding \"base64\"")
	}
	return map[string]any{
		"path":     target.canonPath,
		"encoding": "utf8",
		"content":  string(data),
		"size":     len(data),
	}, nil
}

func (rt *Runtime) fsWrite(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path                 string  `json:"path"`
		Content              *string `json:"content"`
		ContentBase64        *string `json:"content_base64"`
		Atomic               bool    `json:"atomic"`
		MkdirParents         bool    `json:"mkdir_parents"`
		ExpectedRoot         string  `json:"expected_root"`
		ExpectedRootIdentity string  `json:"expected_root_identity"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.write requires non-empty path")
	}
	if (p.Content == nil) == (p.ContentBase64 == nil) {
		return nil, rpcErr(-32602, "host.fs.write requires exactly one of content or content_base64")
	}
	var data []byte
	if p.ContentBase64 != nil {
		decoded, err := base64.StdEncoding.DecodeString(*p.ContentBase64)
		if err != nil {
			return nil, rpcErr(-32602, "invalid content_base64: "+err.Error())
		}
		data = decoded
	} else {
		data = []byte(*p.Content)
	}
	if int64(len(data)) > maxFSWriteLimit {
		return nil, rpcErr(-32602, "content exceeds maximum write size")
	}

	target, rerr := rt.resolveFSScope(p.Path, true)
	if rerr != nil {
		return nil, rerr
	}
	if target.rel == "." {
		return nil, rpcErr(-32602, "cannot overwrite directory root")
	}
	root, cleanup, rerr := rt.openVerifiedFSOperationRoot(p.ExpectedRoot, p.ExpectedRootIdentity, target)
	if rerr != nil {
		return nil, rerr
	}
	defer cleanup()

	if p.MkdirParents && target.isDirRoot {
		parentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(target.rel)))
		if parentRel != "." && parentRel != "" {
			if err := root.MkdirAll(parentRel, 0o755); err != nil {
				return nil, rpcErr(-32004, err.Error())
			}
		}
	}
	if p.Atomic && target.isDirRoot {
		if err := writeAtomicInRoot(root, target.rel, data); err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
	} else {
		f, err := root.OpenFile(target.rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return nil, rpcErr(-32004, err.Error())
		}
		if err := f.Close(); err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
	}
	return map[string]any{
		"ok":   true,
		"path": target.canonPath,
		"size": len(data),
	}, nil
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func createTempInRoot(root *os.Root, targetRel string) (*os.File, string, error) {
	slashRel := filepath.ToSlash(targetRel)
	dir := "."
	base := slashRel
	if idx := strings.LastIndexByte(slashRel, '/'); idx >= 0 {
		dir = slashRel[:idx]
		base = slashRel[idx+1:]
	}
	prefix := fmt.Sprintf(".%s.tmp-%s", base, randomHex(6))
	tmpRel := prefix
	if dir != "." && dir != "" {
		tmpRel = dir + "/" + prefix
	}
	f, err := root.OpenFile(tmpRel, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, tmpRel, nil
}

func writeAtomicInRoot(root *os.Root, targetRel string, data []byte) error {
	f, tmpRel, err := createTempInRoot(root, targetRel)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = f.Close()
			_ = root.Remove(tmpRel)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := root.Rename(tmpRel, targetRel); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (rt *Runtime) fsMkdir(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		Parents   bool   `json:"parents"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.mkdir requires non-empty path")
	}
	target, rerr := rt.resolveFSScope(p.Path, true)
	if rerr != nil {
		return nil, rerr
	}
	if !target.isDirRoot {
		return nil, rpcErr(-32602, "cannot mkdir on a file grant")
	}
	root, err := os.OpenRoot(target.rootDir)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	defer root.Close()
	if target.rel == "." {
		return map[string]any{"ok": true, "path": target.canonPath}, nil
	}
	if p.Recursive || p.Parents {
		err = root.MkdirAll(target.rel, 0o755)
	} else {
		err = root.Mkdir(target.rel, 0o755)
	}
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	return map[string]any{
		"ok":   true,
		"path": target.canonPath,
	}, nil
}

func (rt *Runtime) fsDelete(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path                 string `json:"path"`
		Recursive            bool   `json:"recursive"`
		ExpectedRoot         string `json:"expected_root"`
		ExpectedRootIdentity string `json:"expected_root_identity"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.delete requires non-empty path")
	}
	target, rerr := rt.resolveFSScope(p.Path, true)
	if rerr != nil {
		return nil, rerr
	}
	if target.isDirRoot && target.rel == "." {
		return nil, rpcErr(-32001, "cannot delete authorized root directory itself")
	}
	root, cleanup, rerr := rt.openVerifiedFSOperationRoot(p.ExpectedRoot, p.ExpectedRootIdentity, target)
	if rerr != nil {
		return nil, rerr
	}
	defer cleanup()

	var err error
	if p.Recursive {
		err = root.RemoveAll(target.rel)
	} else {
		err = root.Remove(target.rel)
	}
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	return map[string]any{
		"ok":   true,
		"path": target.canonPath,
	}, nil
}

func (rt *Runtime) resolveDataDirChildRel(rawPath string) (dataRoot *os.Root, targetDisplay string, relSlash string, rerr *rpcError) {
	if strings.TrimSpace(rawPath) == "" || strings.ContainsRune(rawPath, 0) || !utf8.ValidString(rawPath) {
		return nil, "", "", rpcErr(-32602, "invalid path")
	}
	if !filepath.IsAbs(rawPath) {
		return nil, "", "", rpcErr(-32602, "path must be absolute")
	}
	for _, seg := range strings.FieldsFunc(rawPath, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return nil, "", "", rpcErr(-32602, "path must not contain '..' segments")
		}
	}
	dataRoot, dataAbs, dataCanon, dErr := rt.ensureDataRoot()
	if dErr != nil {
		return nil, "", "", dErr
	}
	targetAbs, err := filepath.Abs(rawPath)
	if err != nil {
		return nil, "", "", rpcErr(-32602, err.Error())
	}
	targetAbs = filepath.Clean(targetAbs)

	rel, ok := pathWithinDir(targetAbs, dataCanon)
	if !ok && dataAbs != "" && dataAbs != dataCanon {
		rel, ok = pathWithinDir(targetAbs, dataAbs)
	}
	if !ok {
		return nil, "", "", rpcErr(-32001, "path is outside plugin data_dir")
	}
	if rel == "." || rel == "" {
		return nil, "", "", rpcErr(-32001, "cannot delete plugin data_dir itself")
	}
	if reserved, blocked := matchProtectedDataFile(rel); blocked {
		return nil, "", "", rpcErr(-32001, "cannot modify "+reserved+" directly")
	}
	return dataRoot, filepath.Join(dataCanon, filepath.FromSlash(rel)), rel, nil
}

func readRootDirNamesNoSymlink(dir *os.Root) ([]string, error) {
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(0)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func openVerifiedDataDirHandle(parent *os.Root, name, relPath string, protected []protectedDataFile) (*os.Root, os.FileInfo, *rpcError) {
	info, err := parent.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, rpcErr(-32004, err.Error())
	}
	if reserved, blocked := isProtectedDataTarget(protected, relPath, info); blocked {
		return nil, nil, rpcErr(-32001, "cannot modify "+reserved+" directly")
	}
	if files.IsSymlinkOrReparseInfo(info) {
		return nil, nil, rpcErr(-32004, "refusing to traverse symlink or junction in data_dir")
	}
	if !info.IsDir() {
		return nil, info, rpcErr(-32004, "parent path is not a directory")
	}
	sub, err := parent.OpenRoot(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, rpcErr(-32004, err.Error())
	}
	subInfo, sErr := sub.Stat(".")
	if sErr != nil || files.IsSymlinkOrReparseInfo(subInfo) || !subInfo.IsDir() || !os.SameFile(info, subInfo) {
		_ = sub.Close()
		if sErr != nil && !errors.Is(sErr, os.ErrNotExist) {
			return nil, nil, rpcErr(-32004, sErr.Error())
		}
		return nil, nil, rpcErr(-32004, "directory identity changed while opening in data_dir")
	}
	return sub, info, nil
}

// deleteDataTreeInRoot walks and deletes a subtree in a single traversal along opened
// *os.Root directory handles (no validation pass followed by path re-traversal).
// Protected files are matched via os.SameFile as well as name to block Windows 8.3 short names.
// Millisecond external local swaps between Lstat and Remove remain outside threat model (不在威胁模型内).
func deleteDataTreeInRoot(parent *os.Root, name, relPrefix string, preInfo os.FileInfo, depth int, protected []protectedDataFile) *rpcError {
	if depth > 64 {
		return rpcErr(-32004, "directory nesting limit exceeded")
	}
	relPath := name
	if relPrefix != "" {
		relPath = relPrefix + "/" + name
	}
	info := preInfo
	if info == nil {
		var err error
		info, err = parent.Lstat(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return rpcErr(-32004, err.Error())
		}
	}
	if reserved, blocked := isProtectedDataTarget(protected, relPath, info); blocked {
		return rpcErr(-32001, "cannot modify "+reserved+" directly")
	}
	if files.IsSymlinkOrReparseInfo(info) {
		return rpcErr(-32004, "refusing to delete symlink or junction in data_dir")
	}
	if info.IsDir() {
		sub, err := parent.OpenRoot(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return rpcErr(-32004, err.Error())
		}
		subInfo, sErr := sub.Stat(".")
		if sErr != nil || files.IsSymlinkOrReparseInfo(subInfo) || !subInfo.IsDir() || !os.SameFile(info, subInfo) {
			_ = sub.Close()
			return rpcErr(-32004, "directory identity changed during deletion")
		}
		names, rErr := readRootDirNamesNoSymlink(sub)
		if rErr != nil {
			_ = sub.Close()
			return rpcErr(-32004, rErr.Error())
		}
		for _, cn := range names {
			if rerr := deleteDataTreeInRoot(sub, cn, relPath, nil, depth+1, protected); rerr != nil {
				_ = sub.Close()
				return rerr
			}
		}
		_ = sub.Close()
	}
	curInfo, err := parent.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return rpcErr(-32004, err.Error())
	}
	if reserved, blocked := isProtectedDataTarget(protected, relPath, curInfo); blocked {
		return rpcErr(-32001, "cannot modify "+reserved+" directly")
	}
	if files.IsSymlinkOrReparseInfo(curInfo) || !os.SameFile(info, curInfo) {
		return rpcErr(-32004, "entry identity changed before removal")
	}
	if err := parent.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return rpcErr(-32004, err.Error())
	}
	return nil
}

func (rt *Runtime) fsDeleteData(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Path) == "" {
		return nil, rpcErr(-32602, "host.fs.delete_data requires non-empty path")
	}
	dataRoot, targetDisplay, relSlash, rerr := rt.resolveDataDirChildRel(p.Path)
	if rerr != nil {
		return nil, rerr
	}
	if rerr := rt.verifyDataRootStartupIdentity(dataRoot); rerr != nil {
		return nil, rerr
	}
	protected := collectProtectedDataFiles(dataRoot)

	segs := strings.Split(filepath.ToSlash(relSlash), "/")
	opened := make([]*os.Root, 0, len(segs)-1)
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}()
	cur := dataRoot
	prefix := ""
	for _, seg := range segs[:len(segs)-1] {
		if seg == "" || seg == "." || seg == ".." {
			return nil, rpcErr(-32602, "invalid path segment")
		}
		if prefix == "" {
			prefix = seg
		} else {
			prefix += "/" + seg
		}
		next, _, openErr := openVerifiedDataDirHandle(cur, seg, prefix, protected)
		if openErr != nil {
			return nil, openErr
		}
		if next == nil {
			return map[string]any{"ok": true, "deleted": false, "path": targetDisplay}, nil
		}
		opened = append(opened, next)
		cur = next
	}

	leaf := segs[len(segs)-1]
	if leaf == "" || leaf == "." || leaf == ".." {
		return nil, rpcErr(-32602, "invalid target name")
	}
	leafInfo, err := cur.Lstat(leaf)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"ok": true, "deleted": false, "path": targetDisplay}, nil
		}
		return nil, rpcErr(-32004, err.Error())
	}
	if reserved, blocked := isProtectedDataTarget(protected, relSlash, leafInfo); blocked {
		return nil, rpcErr(-32001, "cannot modify "+reserved+" directly")
	}
	if files.IsSymlinkOrReparseInfo(leafInfo) {
		return nil, rpcErr(-32004, "refusing to delete symlink or junction in data_dir")
	}
	if !leafInfo.IsDir() || !p.Recursive {
		if err := cur.Remove(leaf); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return map[string]any{"ok": true, "deleted": false, "path": targetDisplay}, nil
			}
			return nil, rpcErr(-32004, err.Error())
		}
		return map[string]any{"ok": true, "deleted": true, "path": targetDisplay}, nil
	}

	if rerr := deleteDataTreeInRoot(cur, leaf, prefix, leafInfo, 0, protected); rerr != nil {
		return nil, rerr
	}
	return map[string]any{
		"ok":      true,
		"deleted": true,
		"path":    targetDisplay,
	}, nil
}

func (rt *Runtime) fsRename(params json.RawMessage) (any, *rpcError) {
	var p struct {
		From                 string `json:"from"`
		To                   string `json:"to"`
		Overwrite            bool   `json:"overwrite"`
		MkdirParents         bool   `json:"mkdir_parents"`
		ExpectedRoot         string `json:"expected_root"`
		ExpectedRootIdentity string `json:"expected_root_identity"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.From) == "" || strings.TrimSpace(p.To) == "" {
		return nil, rpcErr(-32602, "host.fs.rename requires non-empty from and to")
	}
	fromTarget, rerr := rt.resolveFSScope(p.From, true)
	if rerr != nil {
		return nil, rerr
	}
	toTarget, rerr := rt.resolveFSScope(p.To, true)
	if rerr != nil {
		return nil, rerr
	}
	if (fromTarget.isDirRoot && fromTarget.rel == ".") || (toTarget.isDirRoot && toTarget.rel == ".") {
		return nil, rpcErr(-32001, "cannot rename authorized root directory itself")
	}
	if !fromTarget.isDirRoot || !toTarget.isDirRoot {
		return nil, rpcErr(-32602, "host.fs.rename requires paths inside directory grants")
	}

	toRoot, cleanup, rerr := rt.openVerifiedFSOperationRoot(p.ExpectedRoot, p.ExpectedRootIdentity, toTarget)
	if rerr != nil {
		return nil, rerr
	}
	defer cleanup()
	if fromRel, ok := pathWithinDir(fromTarget.canonPath, toTarget.rootDir); ok && fromRel != "." {
		fromTarget.rootDir = toTarget.rootDir
		fromTarget.rel = fromRel
		fromTarget.isDirRoot = true
	}

	if fromTarget.rootDir == toTarget.rootDir {
		if p.MkdirParents {
			parentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(toTarget.rel)))
			if parentRel != "." && parentRel != "" {
				if err := toRoot.MkdirAll(parentRel, 0o755); err != nil {
					return nil, rpcErr(-32004, err.Error())
				}
			}
		}
		if !p.Overwrite {
			if _, err := toRoot.Lstat(toTarget.rel); err == nil {
				return nil, rpcErr(-32004, os.ErrExist.Error())
			}
		}
		if err := toRoot.Rename(fromTarget.rel, toTarget.rel); err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
	} else {
		if rerr := renameCrossRoot(fromTarget, toTarget, toRoot, p.Overwrite, p.MkdirParents); rerr != nil {
			return nil, rerr
		}
	}
	return map[string]any{
		"ok":   true,
		"from": fromTarget.canonPath,
		"to":   toTarget.canonPath,
	}, nil
}

func (rt *Runtime) fsCopy(params json.RawMessage) (any, *rpcError) {
	var p struct {
		From                 string `json:"from"`
		To                   string `json:"to"`
		Overwrite            bool   `json:"overwrite"`
		MkdirParents         bool   `json:"mkdir_parents"`
		ExpectedRoot         string `json:"expected_root"`
		ExpectedRootIdentity string `json:"expected_root_identity"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.From) == "" || strings.TrimSpace(p.To) == "" {
		return nil, rpcErr(-32602, "host.fs.copy requires non-empty from and to")
	}
	fromTarget, rerr := rt.resolveFSScope(p.From, false)
	if rerr != nil {
		return nil, rerr
	}
	toTarget, rerr := rt.resolveFSScope(p.To, true)
	if rerr != nil {
		return nil, rerr
	}
	if (fromTarget.isDirRoot && fromTarget.rel == ".") || (toTarget.isDirRoot && toTarget.rel == ".") {
		return nil, rpcErr(-32001, "cannot copy authorized root directory itself")
	}
	if !toTarget.isDirRoot {
		return nil, rpcErr(-32602, "host.fs.copy requires destination inside a directory grant")
	}
	if fromTarget.canonPath == toTarget.canonPath {
		return nil, rpcErr(-32602, "source and destination cannot be the same path")
	}

	toRoot, cleanup, rerr := rt.openVerifiedFSOperationRoot(p.ExpectedRoot, p.ExpectedRootIdentity, toTarget)
	if rerr != nil {
		return nil, rerr
	}
	defer cleanup()

	written, rerr := copyFileBetweenRoots(fromTarget, toTarget, toRoot, p.Overwrite, p.MkdirParents)
	if rerr != nil {
		return nil, rerr
	}
	return map[string]any{
		"ok":   true,
		"from": fromTarget.canonPath,
		"to":   toTarget.canonPath,
		"size": written,
	}, nil
}

func copyFileBetweenRoots(fromTarget, toTarget *resolvedFSTarget, toRoot *os.Root, overwrite, mkdirParents bool) (int64, *rpcError) {
	fromRoot, err := os.OpenRoot(fromTarget.rootDir)
	if err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	defer fromRoot.Close()

	srcInfo, err := fromRoot.Lstat(fromTarget.rel)
	if err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 || srcInfo.IsDir() || !srcInfo.Mode().IsRegular() {
		return 0, rpcErr(-32602, "file copy only supports regular non-symlink files")
	}
	srcFile, actualInfo, err := openPluginFile(fromRoot, fromTarget.rel)
	if err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	defer func() {
		_ = srcFile.Close()
	}()

	if mkdirParents {
		parentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(toTarget.rel)))
		if parentRel != "." && parentRel != "" {
			if err := toRoot.MkdirAll(parentRel, 0o755); err != nil {
				return 0, rpcErr(-32004, err.Error())
			}
		}
	}
	if err := checkPluginParents(toRoot, toTarget.rel); err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	if dstInfo, err := toRoot.Lstat(toTarget.rel); err == nil {
		if !overwrite {
			return 0, rpcErr(-32004, os.ErrExist.Error())
		}
		if dstInfo.Mode()&os.ModeSymlink != 0 || dstInfo.IsDir() || !dstInfo.Mode().IsRegular() {
			return 0, rpcErr(-32602, "cannot overwrite directory or symlink")
		}
	}

	tmpFile, tmpRel, err := createTempInRoot(toRoot, toTarget.rel)
	if err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmpFile.Close()
			_ = toRoot.Remove(tmpRel)
		}
	}()

	written, err := io.Copy(tmpFile, srcFile)
	if err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	if err := tmpFile.Chmod(actualInfo.Mode().Perm()); err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	if err := tmpFile.Sync(); err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	if err := tmpFile.Close(); err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	if err := toRoot.Rename(tmpRel, toTarget.rel); err != nil {
		return 0, rpcErr(-32004, err.Error())
	}
	cleanup = false
	return written, nil
}

func renameCrossRoot(fromTarget, toTarget *resolvedFSTarget, toRoot *os.Root, overwrite, mkdirParents bool) *rpcError {
	if _, rerr := copyFileBetweenRoots(fromTarget, toTarget, toRoot, overwrite, mkdirParents); rerr != nil {
		return rerr
	}
	fromRoot, err := os.OpenRoot(fromTarget.rootDir)
	if err != nil {
		return rpcErr(-32004, err.Error())
	}
	defer fromRoot.Close()
	if err := fromRoot.Remove(fromTarget.rel); err != nil {
		return rpcErr(-32004, err.Error())
	}
	return nil
}

func (rt *Runtime) fsAtomicSwap(params json.RawMessage) (any, *rpcError) {
	var p struct {
		PathA                string `json:"path_a"`
		PathB                string `json:"path_b"`
		ExpectedRoot         string `json:"expected_root"`
		ExpectedRootIdentity string `json:"expected_root_identity"`
	}
	if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.PathA) == "" || strings.TrimSpace(p.PathB) == "" {
		return nil, rpcErr(-32602, "host.fs.atomic_swap requires non-empty path_a and path_b")
	}
	targetA, rerr := rt.resolveFSScope(p.PathA, true)
	if rerr != nil {
		return nil, rerr
	}
	targetB, rerr := rt.resolveFSScope(p.PathB, true)
	if rerr != nil {
		return nil, rerr
	}
	if !targetA.isDirRoot || !targetB.isDirRoot || targetA.rel == "." || targetB.rel == "." {
		return nil, rpcErr(-32602, "host.fs.atomic_swap requires files inside authorized directories")
	}
	if filepath.Dir(targetA.canonPath) != filepath.Dir(targetB.canonPath) || targetA.rootDir != targetB.rootDir {
		return nil, rpcErr(-32602, "host.fs.atomic_swap requires both paths to be in the same directory")
	}
	if targetA.canonPath == targetB.canonPath {
		return nil, rpcErr(-32602, "path_a and path_b must differ")
	}

	root, cleanup, rerr := rt.openVerifiedFSOperationRoot(p.ExpectedRoot, p.ExpectedRootIdentity, targetA, targetB)
	if rerr != nil {
		return nil, rerr
	}
	defer cleanup()

	parentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(targetA.rel)))
	parentRoot := root
	if parentRel != "." && parentRel != "" {
		pr, err := root.OpenRoot(parentRel)
		if err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
		defer pr.Close()
		parentRoot = pr
	}

	baseA := filepath.Base(targetA.canonPath)
	baseB := filepath.Base(targetB.canonPath)
	swapped, err := fsSwapFilesInDir(parentRoot, baseA, baseB)
	if err != nil {
		return nil, rpcErr(-32004, err.Error())
	}
	if !swapped {
		if err := fallbackSwapFilesInDir(parentRoot, baseA, baseB); err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
	}
	return map[string]any{
		"ok":     true,
		"atomic": swapped,
		"path_a": targetA.canonPath,
		"path_b": targetB.canonPath,
	}, nil
}

func fallbackSwapFilesInDir(dir *os.Root, a, b string) error {
	if _, err := dir.Lstat(a); err != nil {
		return err
	}
	if _, err := dir.Lstat(b); err != nil {
		return err
	}
	var backup string
	for range 20 {
		cand := ".block-swap-" + randomHex(8) + ".tmp"
		if _, err := dir.Lstat(cand); errors.Is(err, os.ErrNotExist) {
			backup = cand
			break
		}
	}
	if backup == "" {
		return errors.New("failed to allocate swap temp name")
	}
	if err := dir.Rename(a, backup); err != nil {
		return err
	}
	if err := dir.Rename(b, a); err != nil {
		_ = dir.Rename(backup, a)
		return err
	}
	if err := dir.Rename(backup, b); err != nil {
		_ = dir.Rename(a, b)
		_ = dir.Rename(backup, a)
		return err
	}
	return nil
}

// openFSReadFile opens a regular file inside the plugin's authorized read scope.
func (rt *Runtime) openFSReadFile(rawPath string) (*os.File, os.FileInfo, *os.Root, string, *rpcError) {
	if !rt.manifest.HasPermission(PermSystemFS) {
		return nil, nil, nil, "", missingPerm(PermSystemFS)
	}
	target, rerr := rt.resolveFSScope(rawPath, false)
	if rerr != nil {
		return nil, nil, nil, "", rerr
	}
	root, err := os.OpenRoot(target.rootDir)
	if err != nil {
		return nil, nil, nil, "", rpcErr(-32004, err.Error())
	}
	f, info, err := openPluginFile(root, target.rel)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, "", rpcErr(-32004, err.Error())
	}
	return f, info, root, target.canonPath, nil
}

// openFSWriteRoot resolves a writable target and returns its os.Root and relative path.
func (rt *Runtime) openFSWriteRoot(rawPath string, mkdirParents bool) (*os.Root, *resolvedFSTarget, *rpcError) {
	if !rt.manifest.HasPermission(PermSystemFS) {
		return nil, nil, missingPerm(PermSystemFS)
	}
	target, rerr := rt.resolveFSScope(rawPath, true)
	if rerr != nil {
		return nil, nil, rerr
	}
	if target.rel == "." {
		return nil, nil, rpcErr(-32602, "cannot overwrite directory root")
	}
	root, err := os.OpenRoot(target.rootDir)
	if err != nil {
		return nil, nil, rpcErr(-32004, err.Error())
	}
	if mkdirParents && target.isDirRoot {
		parentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(target.rel)))
		if parentRel != "." && parentRel != "" {
			if err := root.MkdirAll(parentRel, 0o755); err != nil {
				_ = root.Close()
				return nil, nil, rpcErr(-32004, err.Error())
			}
		}
	}
	return root, target, nil
}
