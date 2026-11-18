#!/usr/bin/env python3
"""Official block-sync plugin for Block (blockmd)."""

import base64
from datetime import datetime
import hashlib
import json
import os
import shutil
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET


VALID_PROTOCOLS = ("local_mirror", "webdav", "git")
VALID_CONFLICT_POLICIES = ("keep_both", "remote_first", "local_first")
MAX_LOG_ENTRIES = 20

DEFAULT_CONFIG = {
    "protocol": "local_mirror",
    "target_path": "",
    "secret_token": "",
    "auto_sync": True,
    "conflict_policy": "keep_both",
}


class HostBridge:
    """Line-delimited JSON-RPC 2.0 bridge between plugin and Block host."""

    def __init__(self):
        self._seq = 1000

    def send(self, obj):
        sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
        sys.stdout.flush()

    def call_host(self, method, params=None):
        self._seq += 1
        req_id = f"p-{self._seq}"
        self.send({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})
        while True:
            line = sys.stdin.readline()
            if not line:
                raise RuntimeError("stdin closed while awaiting host response")
            line = line.strip()
            if not line:
                continue
            msg = json.loads(line)
            if str(msg.get("id")) == req_id:
                if msg.get("error"):
                    raise RuntimeError(msg["error"].get("message", "host error"))
                return msg.get("result", {})

    def serve(self, handler):
        while True:
            line = sys.stdin.readline()
            if not line:
                break
            line = line.strip()
            if not line:
                continue
            req = json.loads(line)
            method = req.get("method")
            req_id = req.get("id")
            try:
                result = handler(self, method, req.get("params") or {})
                self.send({"jsonrpc": "2.0", "id": req_id, "result": result})
                if method == "plugin.shutdown":
                    break
            except Exception as exc:
                self.send({
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "error": {"code": -32000, "message": str(exc)},
                })


def sha256_text(text):
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def now_hms():
    return datetime.now().strftime("%H:%M:%S")


def conflict_stamp():
    return datetime.now().strftime("%Y%m%d-%H%M%S")


def is_safe_rel_md(rel_path):
    if not rel_path or rel_path.startswith("/") or rel_path.startswith("\\"):
        return False
    norm = rel_path.replace("\\", "/")
    parts = norm.split("/")
    if any(part in ("", ".", "..") or part.startswith(".") for part in parts):
        return False
    return norm.endswith(".md")


def make_conflict_path(rel_path, stamp):
    norm = rel_path.replace("\\", "/")
    if norm.endswith(".md"):
        return f"{norm[:-3]}.conflict-{stamp}.md"
    return f"{norm}.conflict-{stamp}.md"


def sanitize_error_text(exc):
    msg = str(exc).strip().replace("\n", " ")
    if len(msg) > 120:
        msg = msg[:117] + "..."
    return msg or exc.__class__.__name__


class SyncPlugin:
    def __init__(self):
        self.plugin_id = "block-sync"
        self.data_dir = ""
        self.notebook_root = ""
        self.locale = "zh"
        self.config = dict(DEFAULT_CONFIG)
        self.stats = {"total_synced": 0, "last_sync": ""}
        self.logs = []
        self.sync_meta = {}
        self.last_status = "本地镜像 · 已就绪"

    def _load_state(self, bridge):
        cfg_res = bridge.call_host("host.storage.get", {"key": "config"})
        if cfg_res.get("found") and isinstance(cfg_res.get("value"), dict):
            self.config = self._normalize_config(cfg_res["value"])

        stats_res = bridge.call_host("host.storage.get", {"key": "stats"})
        if stats_res.get("found") and isinstance(stats_res.get("value"), dict):
            val = stats_res["value"]
            self.stats = {
                "total_synced": int(val.get("total_synced", 0) or 0),
                "last_sync": str(val.get("last_sync", "") or ""),
            }

        logs_res = bridge.call_host("host.storage.get", {"key": "logs"})
        if logs_res.get("found") and isinstance(logs_res.get("value"), list):
            self.logs = [self._normalize_log(entry) for entry in logs_res["value"] if isinstance(entry, dict)][:MAX_LOG_ENTRIES]

        meta_res = bridge.call_host("host.storage.get", {"key": "sync_meta"})
        if meta_res.get("found") and isinstance(meta_res.get("value"), dict):
            self.sync_meta = meta_res["value"]

        self.last_status = f"{self._protocol_label(self.config['protocol'])} · 已就绪"

    def _normalize_log(self, entry):
        title = str(entry.get("title") or "同步事件").strip() or "同步事件"
        time_str = str(entry.get("time") or now_hms()).strip() or now_hms()
        status = str(entry.get("status") or "").strip()
        item = {"title": title, "time": time_str}
        if status:
            item["status"] = status
        return item

    def _normalize_config(self, raw):
        merged = dict(DEFAULT_CONFIG)
        merged.update(self.config)
        if not isinstance(raw, dict):
            return merged

        protocol = raw.get("protocol", merged["protocol"])
        if protocol in VALID_PROTOCOLS:
            merged["protocol"] = protocol

        if "target_path" in raw and raw["target_path"] is not None:
            merged["target_path"] = str(raw["target_path"]).strip()
        elif not merged["target_path"] and raw.get("endpoint_url"):
            merged["target_path"] = str(raw["endpoint_url"]).strip()

        if "secret_token" in raw and raw["secret_token"] is not None:
            merged["secret_token"] = str(raw["secret_token"])

        if "auto_sync" in raw:
            merged["auto_sync"] = bool(raw["auto_sync"])

        policy = raw.get("conflict_policy", merged["conflict_policy"])
        if policy in VALID_CONFLICT_POLICIES:
            merged["conflict_policy"] = policy

        return merged

    def _save_config(self, bridge):
        bridge.call_host("host.storage.set", {"key": "config", "value": self.config})

    def _save_runtime(self, bridge):
        bridge.call_host("host.storage.set", {"key": "stats", "value": self.stats})
        bridge.call_host("host.storage.set", {"key": "logs", "value": self.logs[:MAX_LOG_ENTRIES]})
        bridge.call_host("host.storage.set", {"key": "sync_meta", "value": self.sync_meta})

    def _append_log(self, title, status):
        entry = {"title": str(title) or "同步任务", "time": now_hms(), "status": str(status) or "完成"}
        self.logs.insert(0, entry)
        self.logs = self.logs[:MAX_LOG_ENTRIES]

    def _protocol_label(self, protocol):
        return {
            "local_mirror": "目录镜像",
            "webdav": "WebDAV",
            "git": "Git 仓库",
        }.get(protocol, "目录镜像")

    def _resolve_mirror_dir(self):
        raw = self.config.get("target_path", "").strip()
        base = self.data_dir or os.getcwd()
        if not raw:
            return os.path.join(base, "mirror")
        expanded = os.path.expanduser(raw)
        if os.path.isabs(expanded):
            return os.path.normpath(expanded)
        return os.path.normpath(os.path.join(base, expanded))

    def _read_local_notes(self, bridge, only_path=None):
        notes = {}
        listed = bridge.call_host("host.notes.list", {}).get("notes") or []
        for item in listed:
            rel_path = str(item.get("path") or "").replace("\\", "/")
            if not is_safe_rel_md(rel_path):
                continue
            if only_path and rel_path != only_path:
                continue
            res = bridge.call_host("host.notes.read", {"path": rel_path})
            notes[rel_path] = str(res.get("content") or "")
        return notes

    def _resolve_conflict_pair(self, rel_path, local_text, remote_text, base_hash):
        local_hash = sha256_text(local_text)
        remote_hash = sha256_text(remote_text)
        if local_hash == remote_hash:
            return "skip", local_text, None, local_hash

        if base_hash and base_hash == local_hash:
            return "pull", remote_text, None, remote_hash
        if base_hash and base_hash == remote_hash:
            return "push", local_text, None, local_hash

        policy = self.config.get("conflict_policy", "keep_both")
        if policy == "local_first":
            return "push", local_text, None, local_hash
        if policy == "remote_first":
            return "pull", remote_text, None, remote_hash

        # keep_both: preserve remote copy with conflict suffix and keep local as main
        conflict_rel = make_conflict_path(rel_path, conflict_stamp())
        return "keep_both", local_text, (conflict_rel, remote_text), local_hash

    # ---------- Local Mirror Mode ----------

    def _test_local_mirror(self):
        mirror_dir = self._resolve_mirror_dir()
        try:
            os.makedirs(mirror_dir, exist_ok=True)
            probe_file = os.path.join(mirror_dir, ".block-sync-probe")
            with open(probe_file, "w", encoding="utf-8") as fh:
                fh.write("ok")
            os.remove(probe_file)
            return True, f"目录镜像可用: {mirror_dir}"
        except Exception as exc:
            return False, f"目录镜像不可写: {sanitize_error_text(exc)}"

    def _scan_mirror_files(self, mirror_dir, only_path=None):
        remote_files = {}
        if not os.path.isdir(mirror_dir):
            return remote_files
        if only_path:
            candidate = os.path.join(mirror_dir, *only_path.split("/"))
            if os.path.isfile(candidate):
                with open(candidate, "r", encoding="utf-8", errors="replace") as fh:
                    remote_files[only_path] = fh.read()
            return remote_files

        for root, dirs, files in os.walk(mirror_dir):
            dirs[:] = [d for d in dirs if not d.startswith(".")]
            for name in files:
                if name.startswith(".") or not name.endswith(".md"):
                    continue
                full = os.path.join(root, name)
                rel = os.path.relpath(full, mirror_dir).replace(os.sep, "/")
                if not is_safe_rel_md(rel):
                    continue
                with open(full, "r", encoding="utf-8", errors="replace") as fh:
                    remote_files[rel] = fh.read()
        return remote_files

    def _write_mirror_file(self, mirror_dir, rel_path, content):
        dest = os.path.join(mirror_dir, *rel_path.split("/"))
        parent = os.path.dirname(dest)
        if parent:
            os.makedirs(parent, exist_ok=True)
        with open(dest, "w", encoding="utf-8") as fh:
            fh.write(content)

    def _sync_local_mirror(self, bridge, only_path=None):
        mirror_dir = self._resolve_mirror_dir()
        os.makedirs(mirror_dir, exist_ok=True)

        local_notes = self._read_local_notes(bridge, only_path=only_path)
        remote_notes = self._scan_mirror_files(mirror_dir, only_path=only_path)

        meta_key = f"local_mirror:{mirror_dir}"
        base_map = dict(self.sync_meta.get(meta_key) or {})
        synced_count = 0

        all_paths = sorted(set(local_notes.keys()) | set(remote_notes.keys()))
        for rel_path in all_paths:
            in_local = rel_path in local_notes
            in_remote = rel_path in remote_notes
            if in_local and not in_remote:
                text = local_notes[rel_path]
                self._write_mirror_file(mirror_dir, rel_path, text)
                base_map[rel_path] = sha256_text(text)
                synced_count += 1
                self._append_log(rel_path, "已镜像到本地目录")
            elif in_remote and not in_local:
                text = remote_notes[rel_path]
                bridge.call_host("host.notes.write", {"path": rel_path, "content": text})
                base_map[rel_path] = sha256_text(text)
                synced_count += 1
                self._append_log(rel_path, "已从镜像恢复")
            else:
                action, chosen_text, conflict_extra, new_hash = self._resolve_conflict_pair(
                    rel_path, local_notes[rel_path], remote_notes[rel_path], base_map.get(rel_path)
                )
                if action == "skip":
                    base_map[rel_path] = new_hash
                elif action == "push":
                    self._write_mirror_file(mirror_dir, rel_path, chosen_text)
                    base_map[rel_path] = new_hash
                    synced_count += 1
                    self._append_log(rel_path, "本地优先覆盖镜像")
                elif action == "pull":
                    bridge.call_host("host.notes.write", {"path": rel_path, "content": chosen_text})
                    base_map[rel_path] = new_hash
                    synced_count += 1
                    self._append_log(rel_path, "镜像优先更新本地")
                elif action == "keep_both":
                    conflict_rel, conflict_text = conflict_extra
                    bridge.call_host("host.notes.write", {"path": conflict_rel, "content": conflict_text})
                    self._write_mirror_file(mirror_dir, conflict_rel, conflict_text)
                    self._write_mirror_file(mirror_dir, rel_path, chosen_text)
                    base_map[rel_path] = new_hash
                    base_map[conflict_rel] = sha256_text(conflict_text)
                    synced_count += 1
                    self._append_log(rel_path, f"冲突已保留副本 {conflict_rel}")

        self.sync_meta[meta_key] = base_map
        self.stats["total_synced"] += synced_count
        self.stats["last_sync"] = now_hms()
        if synced_count == 0 and not all_paths:
            return "当前笔记本暂无笔记，镜像目录已就绪"
        if synced_count == 0:
            return "目录镜像已是最新，无需更新"
        return f"已完成目录镜像同步，处理 {synced_count} 篇笔记"

    # ---------- WebDAV Mode ----------

    def _webdav_headers(self, extra=None):
        headers = {"User-Agent": "block-sync/1.0"}
        token = self.config.get("secret_token", "").strip()
        if token:
            if token.lower().startswith("bearer ") or token.lower().startswith("basic "):
                headers["Authorization"] = token
            elif ":" in token:
                encoded = base64.b64encode(token.encode("utf-8")).decode("ascii")
                headers["Authorization"] = f"Basic {encoded}"
            else:
                headers["Authorization"] = f"Bearer {token}"
        if extra:
            headers.update(extra)
        return headers

    def _webdav_base_url(self):
        url = self.config.get("target_path", "").strip()
        if not url:
            return ""
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme not in ("http", "https") or not parsed.netloc:
            return ""
        return url.rstrip("/") + "/"

    def _webdav_request(self, method, url, body=None, extra_headers=None, timeout=6):
        req = urllib.request.Request(
            url,
            data=body,
            headers=self._webdav_headers(extra_headers),
            method=method,
        )
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()

    def _test_webdav(self):
        base_url = self._webdav_base_url()
        if not base_url:
            return False, "请填写有效的 WebDAV http(s) 地址"
        try:
            status, _ = self._webdav_request("PROPFIND", base_url, extra_headers={"Depth": "0"})
            if status in (200, 204, 207):
                return True, f"WebDAV 连接正常 (HTTP {status})"
            return False, f"WebDAV 响应异常 (HTTP {status})"
        except urllib.error.HTTPError as exc:
            if exc.code in (401, 403):
                return False, f"WebDAV 认证失败 (HTTP {exc.code})"
            return False, f"WebDAV 请求失败 (HTTP {exc.code})"
        except Exception as exc:
            return False, f"WebDAV 连接不可达: {sanitize_error_text(exc)}"

    def _webdav_list_md(self, base_url):
        status, body = self._webdav_request("PROPFIND", base_url, extra_headers={"Depth": "1"})
        if status not in (200, 207):
            return []
        root = ET.fromstring(body)
        base_parsed = urllib.parse.urlparse(base_url)
        base_path = urllib.parse.unquote(base_parsed.path)
        items = []
        for elem in root.iter():
            if elem.tag.endswith("href") and elem.text:
                href_path = urllib.parse.unquote(urllib.parse.urlparse(elem.text.strip()).path)
                if href_path.startswith(base_path):
                    rel = href_path[len(base_path):].lstrip("/")
                    if is_safe_rel_md(rel):
                        items.append(rel)
        return sorted(set(items))

    def _webdav_ensure_parents(self, base_url, rel_path):
        parts = rel_path.split("/")[:-1]
        cur = base_url
        for part in parts:
            cur = cur + urllib.parse.quote(part) + "/"
            try:
                self._webdav_request("MKCOL", cur)
            except urllib.error.HTTPError as exc:
                if exc.code not in (405, 301, 302, 409):
                    raise

    def _webdav_get_text(self, file_url):
        try:
            status, body = self._webdav_request("GET", file_url)
            if 200 <= status < 300:
                return True, body.decode("utf-8", errors="replace")
            return False, ""
        except urllib.error.HTTPError as exc:
            if exc.code == 404:
                return False, ""
            raise

    def _webdav_put_text(self, base_url, rel_path, content):
        self._webdav_ensure_parents(base_url, rel_path)
        file_url = base_url + "/".join(urllib.parse.quote(p) for p in rel_path.split("/"))
        self._webdav_request(
            "PUT",
            file_url,
            body=content.encode("utf-8"),
            extra_headers={"Content-Type": "text/markdown; charset=utf-8"},
        )

    def _sync_webdav(self, bridge, only_path=None):
        base_url = self._webdav_base_url()
        if not base_url:
            msg = "请先填写有效的 WebDAV http(s) 地址"
            self._append_log("WebDAV 同步", msg)
            return msg

        try:
            local_notes = self._read_local_notes(bridge, only_path=only_path)
            remote_paths = [only_path] if only_path else self._webdav_list_md(base_url)
            remote_notes = {}
            for rel_path in remote_paths:
                if not is_safe_rel_md(rel_path):
                    continue
                file_url = base_url + "/".join(urllib.parse.quote(p) for p in rel_path.split("/"))
                found, text = self._webdav_get_text(file_url)
                if found:
                    remote_notes[rel_path] = text

            meta_key = f"webdav:{base_url}"
            base_map = dict(self.sync_meta.get(meta_key) or {})
            synced_count = 0

            all_paths = sorted(set(local_notes.keys()) | set(remote_notes.keys()))
            for rel_path in all_paths:
                in_local = rel_path in local_notes
                in_remote = rel_path in remote_notes
                if in_local and not in_remote:
                    text = local_notes[rel_path]
                    self._webdav_put_text(base_url, rel_path, text)
                    base_map[rel_path] = sha256_text(text)
                    synced_count += 1
                    self._append_log(rel_path, "已上传到 WebDAV")
                elif in_remote and not in_local:
                    text = remote_notes[rel_path]
                    bridge.call_host("host.notes.write", {"path": rel_path, "content": text})
                    base_map[rel_path] = sha256_text(text)
                    synced_count += 1
                    self._append_log(rel_path, "已从 WebDAV 下载")
                else:
                    action, chosen_text, conflict_extra, new_hash = self._resolve_conflict_pair(
                        rel_path, local_notes[rel_path], remote_notes[rel_path], base_map.get(rel_path)
                    )
                    if action == "skip":
                        base_map[rel_path] = new_hash
                    elif action == "push":
                        self._webdav_put_text(base_url, rel_path, chosen_text)
                        base_map[rel_path] = new_hash
                        synced_count += 1
                        self._append_log(rel_path, "本地优先上传 WebDAV")
                    elif action == "pull":
                        bridge.call_host("host.notes.write", {"path": rel_path, "content": chosen_text})
                        base_map[rel_path] = new_hash
                        synced_count += 1
                        self._append_log(rel_path, "远端优先更新本地")
                    elif action == "keep_both":
                        conflict_rel, conflict_text = conflict_extra
                        bridge.call_host("host.notes.write", {"path": conflict_rel, "content": conflict_text})
                        self._webdav_put_text(base_url, conflict_rel, conflict_text)
                        self._webdav_put_text(base_url, rel_path, chosen_text)
                        base_map[rel_path] = new_hash
                        base_map[conflict_rel] = sha256_text(conflict_text)
                        synced_count += 1
                        self._append_log(rel_path, f"WebDAV 冲突已保留副本 {conflict_rel}")

            self.sync_meta[meta_key] = base_map
            self.stats["total_synced"] += synced_count
            self.stats["last_sync"] = now_hms()
            if synced_count == 0:
                return "WebDAV 已是最新状态"
            return f"已完成 WebDAV 同步，处理 {synced_count} 篇笔记"
        except Exception as exc:
            msg = f"WebDAV 同步未完成: {sanitize_error_text(exc)}"
            self._append_log("WebDAV 同步", msg)
            return msg

    # ---------- Git Mode ----------

    def _is_remote_git_url(self, target):
        lower = target.lower()
        return (
            lower.startswith("https://")
            or lower.startswith("http://")
            or lower.startswith("ssh://")
            or lower.startswith("git@")
            or lower.startswith("file://")
        )

    def _git_env(self):
        env = dict(os.environ)
        env["GIT_TERMINAL_PROMPT"] = "0"
        return env

    def _run_git(self, args, cwd=None, timeout=8):
        cmd = ["git"]
        token = self.config.get("secret_token", "").strip()
        target = self.config.get("target_path", "").strip()
        if token and target.lower().startswith("https://"):
            encoded = base64.b64encode(f"x-access-token:{token}".encode("utf-8")).decode("ascii")
            cmd.extend(["-c", f"http.extraHeader=Authorization: Basic {encoded}"])
        cmd.extend(args)
        return subprocess.run(
            cmd,
            cwd=cwd,
            env=self._git_env(),
            capture_output=True,
            text=True,
            timeout=timeout,
        )

    def _resolve_local_git_dir(self):
        target = self.config.get("target_path", "").strip()
        if target and not self._is_remote_git_url(target):
            expanded = os.path.expanduser(target)
            if os.path.isabs(expanded):
                return os.path.normpath(expanded)
            base = self.data_dir or os.getcwd()
            return os.path.normpath(os.path.join(base, expanded))
        return os.path.join(self.data_dir or os.getcwd(), "git-repo")

    def _test_git(self):
        if not shutil.which("git"):
            return False, "当前环境未安装 git 命令"
        target = self.config.get("target_path", "").strip()
        try:
            if target and self._is_remote_git_url(target):
                proc = self._run_git(["ls-remote", "--heads", target])
                if proc.returncode == 0:
                    return True, "Git 远端仓库连接正常"
                err = sanitize_error_text(proc.stderr or proc.stdout or "连接失败")
                return False, f"Git 远端不可达: {err}"

            repo_dir = self._resolve_local_git_dir()
            if not os.path.isdir(repo_dir):
                return False, f"本地 Git 仓库目录不存在: {repo_dir}"
            proc = self._run_git(["rev-parse", "--is-inside-work-tree"], cwd=repo_dir)
            if proc.returncode != 0 or proc.stdout.strip() != "true":
                return False, f"目录不是有效的 Git 仓库: {repo_dir}"
            remote_proc = self._run_git(["remote"], cwd=repo_dir)
            remotes = [r.strip() for r in remote_proc.stdout.splitlines() if r.strip()]
            if remotes:
                ls_proc = self._run_git(["ls-remote", "--heads", remotes[0]], cwd=repo_dir)
                if ls_proc.returncode == 0:
                    return True, f"Git 仓库及远端 {remotes[0]} 正常"
                return True, f"本地 Git 仓库可用（远端 {remotes[0]} 当前离线）"
            return True, f"本地 Git 仓库可用: {repo_dir}"
        except Exception as exc:
            return False, f"Git 检测异常: {sanitize_error_text(exc)}"

    def _sync_git(self, bridge, only_path=None):
        if not shutil.which("git"):
            msg = "当前环境未安装 git 命令"
            self._append_log("Git 同步", msg)
            return msg

        target = self.config.get("target_path", "").strip()
        repo_dir = self._resolve_local_git_dir()

        try:
            if target and self._is_remote_git_url(target):
                if not os.path.isdir(os.path.join(repo_dir, ".git")):
                    os.makedirs(os.path.dirname(repo_dir), exist_ok=True)
                    clone_proc = self._run_git(["clone", target, repo_dir], timeout=12)
                    if clone_proc.returncode != 0:
                        err = sanitize_error_text(clone_proc.stderr or clone_proc.stdout or "clone 失败")
                        msg = f"Git 远端不可达: {err}"
                        self._append_log("Git 同步", msg)
                        return msg
                else:
                    self._run_git(["pull", "--rebase"], cwd=repo_dir, timeout=10)
            else:
                if not os.path.isdir(repo_dir):
                    os.makedirs(repo_dir, exist_ok=True)
                check_proc = self._run_git(["rev-parse", "--is-inside-work-tree"], cwd=repo_dir)
                if check_proc.returncode != 0:
                    init_proc = self._run_git(["init"], cwd=repo_dir)
                    if init_proc.returncode != 0:
                        msg = "Git 仓库初始化失败"
                        self._append_log("Git 同步", msg)
                        return msg

            local_notes = self._read_local_notes(bridge, only_path=only_path)
            remote_notes = self._scan_mirror_files(repo_dir, only_path=only_path)

            meta_key = f"git:{repo_dir}"
            base_map = dict(self.sync_meta.get(meta_key) or {})
            synced_count = 0

            all_paths = sorted(set(local_notes.keys()) | set(remote_notes.keys()))
            for rel_path in all_paths:
                in_local = rel_path in local_notes
                in_remote = rel_path in remote_notes
                if in_local and not in_remote:
                    text = local_notes[rel_path]
                    self._write_mirror_file(repo_dir, rel_path, text)
                    base_map[rel_path] = sha256_text(text)
                    synced_count += 1
                    self._append_log(rel_path, "已写入 Git 仓库")
                elif in_remote and not in_local:
                    text = remote_notes[rel_path]
                    bridge.call_host("host.notes.write", {"path": rel_path, "content": text})
                    base_map[rel_path] = sha256_text(text)
                    synced_count += 1
                    self._append_log(rel_path, "已从 Git 仓库拉取")
                else:
                    action, chosen_text, conflict_extra, new_hash = self._resolve_conflict_pair(
                        rel_path, local_notes[rel_path], remote_notes[rel_path], base_map.get(rel_path)
                    )
                    if action == "skip":
                        base_map[rel_path] = new_hash
                    elif action == "push":
                        self._write_mirror_file(repo_dir, rel_path, chosen_text)
                        base_map[rel_path] = new_hash
                        synced_count += 1
                        self._append_log(rel_path, "本地优先更新 Git 仓库")
                    elif action == "pull":
                        bridge.call_host("host.notes.write", {"path": rel_path, "content": chosen_text})
                        base_map[rel_path] = new_hash
                        synced_count += 1
                        self._append_log(rel_path, "Git 优先更新本地")
                    elif action == "keep_both":
                        conflict_rel, conflict_text = conflict_extra
                        bridge.call_host("host.notes.write", {"path": conflict_rel, "content": conflict_text})
                        self._write_mirror_file(repo_dir, conflict_rel, conflict_text)
                        self._write_mirror_file(repo_dir, rel_path, chosen_text)
                        base_map[rel_path] = new_hash
                        base_map[conflict_rel] = sha256_text(conflict_text)
                        synced_count += 1
                        self._append_log(rel_path, f"Git 冲突已保留副本 {conflict_rel}")

            status_proc = self._run_git(["status", "--porcelain"], cwd=repo_dir)
            if status_proc.returncode == 0 and status_proc.stdout.strip():
                self._run_git(["add", "-A"], cwd=repo_dir)
                self._run_git(
                    [
                        "-c", "user.name=Block Sync",
                        "-c", "user.email=sync@blockmd.local",
                        "commit", "-m", "sync: update markdown notes",
                    ],
                    cwd=repo_dir,
                )
                remote_proc = self._run_git(["remote"], cwd=repo_dir)
                if remote_proc.returncode == 0 and remote_proc.stdout.strip():
                    push_proc = self._run_git(["push"], cwd=repo_dir, timeout=10)
                    if push_proc.returncode != 0:
                        self.sync_meta[meta_key] = base_map
                        self.stats["total_synced"] += synced_count
                        self.stats["last_sync"] = now_hms()
                        return f"已提交到本地 Git，但远端推送未完成: {sanitize_error_text(push_proc.stderr or 'push 失败')}"

            self.sync_meta[meta_key] = base_map
            self.stats["total_synced"] += synced_count
            self.stats["last_sync"] = now_hms()
            if synced_count == 0:
                return "Git 仓库已是最新状态"
            return f"已完成 Git 同步，处理 {synced_count} 篇笔记"
        except Exception as exc:
            msg = f"Git 同步未完成: {sanitize_error_text(exc)}"
            self._append_log("Git 同步", msg)
            return msg

    # ---------- Dispatch & UI ----------

    def _run_sync(self, bridge, only_path=None):
        protocol = self.config.get("protocol", "local_mirror")
        if protocol == "webdav":
            msg = self._sync_webdav(bridge, only_path=only_path)
        elif protocol == "git":
            msg = self._sync_git(bridge, only_path=only_path)
        else:
            msg = self._sync_local_mirror(bridge, only_path=only_path)
        self._save_runtime(bridge)
        self.last_status = msg
        return msg

    def _run_test_conn(self):
        protocol = self.config.get("protocol", "local_mirror")
        if protocol == "webdav":
            ok, msg = self._test_webdav()
        elif protocol == "git":
            ok, msg = self._test_git()
        else:
            ok, msg = self._test_local_mirror()
        self._append_log(f"测试连接 ({self._protocol_label(protocol)})", "通过" if ok else "未连通")
        self.last_status = msg
        return msg

    def build_page(self):
        mode_label = self._protocol_label(self.config.get("protocol", "local_mirror"))
        total_synced = int(self.stats.get("total_synced", 0) or 0)
        kv_rows = [
            {"label": "当前模式", "value": mode_label},
            {"label": "累计同步篇数", "value": f"{total_synced} 篇"},
        ]
        if self.stats.get("last_sync"):
            kv_rows.append({"label": "最近同步时间", "value": self.stats["last_sync"]})

        entries = [self._normalize_log(item) for item in self.logs[:MAX_LOG_ENTRIES]]

        return {
            "title": "同步",
            "status": self.last_status or f"{mode_label} · 已就绪",
            "header_actions": [
                {"id": "sync_now", "label": "立即同步", "variant": "primary"},
            ],
            "cards": [
                {
                    "id": "target-card",
                    "title": "存储目标",
                    "items": [
                        {
                            "kind": "segmented",
                            "id": "protocol",
                            "label": "存储协议",
                            "value": self.config["protocol"],
                            "options": [
                                {"value": "local_mirror", "label": "目录镜像"},
                                {"value": "webdav", "label": "WebDAV"},
                                {"value": "git", "label": "Git 仓库"},
                            ],
                        },
                        {
                            "kind": "input",
                            "id": "target_path",
                            "label": "目标路径或地址",
                            "value": self.config["target_path"],
                            "placeholder": "留空默认使用插件数据目录下的 mirror 文件夹，或填 WebDAV / Git 地址",
                        },
                        {
                            "kind": "secret",
                            "id": "secret_token",
                            "label": "访问凭证",
                            "value": self.config["secret_token"],
                            "placeholder": "WebDAV 可填 user:password 或令牌，Git 可填个人访问令牌",
                        },
                        {
                            "kind": "actions",
                            "buttons": [
                                {"id": "save_config", "label": "保存配置", "variant": "primary"},
                                {"id": "test_conn", "label": "测试连接", "variant": "secondary"},
                            ],
                        },
                    ],
                },
                {
                    "id": "policy-card",
                    "title": "同步策略",
                    "items": [
                        {
                            "kind": "switch",
                            "id": "auto_sync",
                            "label": "保存笔记后自动同步",
                            "value": bool(self.config["auto_sync"]),
                        },
                        {
                            "kind": "segmented",
                            "id": "conflict_policy",
                            "label": "冲突处理",
                            "value": self.config["conflict_policy"],
                            "options": [
                                {"value": "keep_both", "label": "保留双份副本"},
                                {"value": "remote_first", "label": "远端优先"},
                                {"value": "local_first", "label": "本地优先"},
                            ],
                        },
                    ],
                },
                {
                    "id": "status-card",
                    "title": "同步状态与记录",
                    "items": [
                        {
                            "kind": "kv",
                            "rows": kv_rows,
                        },
                        {
                            "kind": "log_list",
                            "empty_text": "暂无同步记录",
                            "entries": entries,
                        },
                    ],
                },
            ],
        }

    def handle(self, bridge, method, params):
        if method == "plugin.initialize":
            self.plugin_id = str(params.get("plugin_id") or "block-sync")
            self.data_dir = str(params.get("data_dir") or "")
            self.notebook_root = str(params.get("notebook_root") or "")
            self.locale = str(params.get("locale") or "zh")
            self._load_state(bridge)
            return {"ready": True}

        if method == "plugin.render":
            return {"page": self.build_page()}

        if method == "plugin.action":
            values = params.get("values") or {}
            if isinstance(values, dict) and values:
                self.config = self._normalize_config(values)
                self._save_config(bridge)

            action_id = str(params.get("action_id") or "")
            if action_id == "save_config":
                self._save_config(bridge)
                self._append_log("保存配置", f"模式: {self._protocol_label(self.config['protocol'])}")
                self._save_runtime(bridge)
                self.last_status = "配置已保存"
                status = "配置已保存"
            elif action_id == "test_conn":
                status = self._run_test_conn()
                self._save_runtime(bridge)
            elif action_id == "sync_now":
                status = self._run_sync(bridge)
            else:
                status = "已完成操作"
                self.last_status = status

            return {
                "values": dict(self.config),
                "page": self.build_page(),
                "status": status,
            }

        if method == "plugin.event":
            event_name = str(params.get("event") or "")
            rel_path = str(params.get("path") or "").replace("\\", "/")
            if event_name == "note.saved" and self.config.get("auto_sync"):
                only_path = rel_path if is_safe_rel_md(rel_path) else None
                try:
                    self._run_sync(bridge, only_path=only_path)
                except Exception as exc:
                    self._append_log(rel_path or "自动同步", f"异常: {sanitize_error_text(exc)}")
                    self._save_runtime(bridge)
            return {"handled": True}

        if method == "plugin.shutdown":
            return {"ok": True}

        raise RuntimeError(f"unsupported method: {method}")


def main():
    plugin = SyncPlugin()
    HostBridge().serve(plugin.handle)


if __name__ == "__main__":
    main()
