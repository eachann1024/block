#!/usr/bin/env python3
from datetime import datetime
import json
import os
import posixpath
import re
import resource
import sys
from urllib.parse import unquote

DEFAULT_CONFIG = {
    "check_orphan_assets": True,
    "check_broken_links_and_empty": True,
    "large_asset_threshold": "5mb",
}

THRESHOLD_BYTES = {
    "1mb": 1024 * 1024,
    "5mb": 5 * 1024 * 1024,
    "10mb": 10 * 1024 * 1024,
}

MD_LINK_RE = re.compile(r'(!?\[[^\]]*\]\()([^)\n]+)(\))')
HTML_REF_RE = re.compile(r'''(?:src|href)\s*=\s*["']([^"']+)["']''', re.IGNORECASE)
WIKI_LINK_RE = re.compile(r'(!?\[\[)([^\]\n]+)(\]\])')

MAX_LOG_ENTRIES = 50


class HostBridge:
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
            msg = json.loads(line)
            if str(msg.get("id")) == req_id:
                if "error" in msg and msg["error"]:
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
                self.send({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32000, "message": str(exc)}})


class DiagnosticsPlugin:
    def __init__(self):
        self.notebook_root = ""
        self.locale = "zh"

    def initialize(self, params):
        self.notebook_root = params.get("notebook_root") or ""
        self.locale = params.get("locale") or "zh"
        return {"ready": True}

    def load_state(self, bridge):
        res = bridge.call_host("host.storage.get", {"key": "diagnostics_state"})
        saved = res.get("value") if res.get("found") and isinstance(res.get("value"), dict) else {}
        config = normalize_config(saved.get("config") or {})
        entries = normalize_entries(saved.get("entries") or [])
        last_status = saved.get("status") if isinstance(saved.get("status"), str) and saved.get("status") else "已就绪"
        return config, entries, last_status

    def save_state(self, bridge, config, entries, status):
        bridge.call_host(
            "host.storage.set",
            {
                "key": "diagnostics_state",
                "value": {
                    "config": config,
                    "entries": entries[:MAX_LOG_ENTRIES],
                    "status": status,
                },
            },
        )

    def collect_workspace_data(self, bridge, read_contents=True):
        notes_res = bridge.call_host("host.notes.list", {})
        notes_meta = notes_res.get("notes") or []
        note_contents = {}
        total_chars = 0
        md_bytes = 0

        for item in notes_meta:
            path = item.get("path") or ""
            size = item.get("size")
            if isinstance(size, int) and size >= 0:
                md_bytes += size
            if read_contents and path:
                read_res = bridge.call_host("host.notes.read", {"path": path})
                content = read_res.get("content") or ""
                note_contents[path] = content
                total_chars += len(content)
                if not isinstance(size, int) or size < 0:
                    md_bytes += len(content.encode("utf-8"))

        assets = scan_assets(self.notebook_root)
        asset_bytes = sum(a["size"] for a in assets)
        rss_bytes = get_process_rss_bytes()

        metrics = {
            "note_count": len(notes_meta),
            "total_chars": total_chars,
            "md_bytes": md_bytes,
            "asset_count": len(assets),
            "asset_bytes": asset_bytes,
            "rss_bytes": rss_bytes,
        }
        return metrics, notes_meta, note_contents, assets

    def run_scan(self, bridge, config):
        metrics, notes_meta, note_contents, assets = self.collect_workspace_data(bridge, read_contents=True)
        now_str = datetime.now().strftime("%H:%M:%S")
        issues = []

        threshold_key = config["large_asset_threshold"]
        threshold_limit = THRESHOLD_BYTES.get(threshold_key, THRESHOLD_BYTES["5mb"])
        for asset in assets:
            if asset["size"] >= threshold_limit:
                issues.append(
                    {
                        "title": f"大体积附件：{asset['rel_path']}（{format_bytes(asset['size'])}）",
                        "time": now_str,
                        "status": "体积超限",
                    }
                )

        note_paths_norm = {normalize_rel_path(item.get("path") or "") for item in notes_meta if item.get("path")}
        referenced_paths = set()
        referenced_basenames = set()

        for note_path, content in note_contents.items():
            note_norm = normalize_rel_path(note_path)
            if config["check_broken_links_and_empty"] and content.strip() == "":
                issues.append(
                    {
                        "title": f"空笔记：{note_norm}",
                        "time": now_str,
                        "status": "空内容",
                    }
                )

            targets = extract_link_targets(content)
            for raw_target, is_wiki in targets:
                resolved, candidate_name = resolve_relative_target(note_norm, raw_target, is_wiki)
                if resolved is not None:
                    referenced_paths.add(resolved)
                if candidate_name:
                    referenced_basenames.add(candidate_name)

                if config["check_broken_links_and_empty"]:
                    if not target_exists(
                        self.notebook_root,
                        note_norm,
                        raw_target,
                        is_wiki,
                        note_paths_norm,
                        assets,
                    ):
                        issues.append(
                            {
                                "title": f"失效链接：{note_norm} -> {raw_target}",
                                "time": now_str,
                                "status": "目标缺失",
                            }
                        )

        if config["check_orphan_assets"] and assets:
            for asset in assets:
                rel_norm = asset["rel_path"]
                base_name = posixpath.basename(rel_norm)
                if rel_norm not in referenced_paths and base_name not in referenced_basenames:
                    issues.append(
                        {
                            "title": f"孤立附件：{rel_norm}（{format_bytes(asset['size'])}）",
                            "time": now_str,
                            "status": "未引用",
                        }
                    )

        if not issues:
            entries = [
                {
                    "title": f"巡检完成：{metrics['note_count']} 篇笔记、{metrics['asset_count']} 个附件均健康",
                    "time": now_str,
                    "status": "通过",
                }
            ]
            status = "巡检通过 · 未发现异常"
        else:
            issue_count = len(issues)
            entries = issues[:MAX_LOG_ENTRIES]
            if issue_count > MAX_LOG_ENTRIES:
                entries[-1] = {
                    "title": f"共发现 {issue_count} 项问题，当前展示前 {MAX_LOG_ENTRIES - 1} 项",
                    "time": now_str,
                    "status": "已截断",
                }
            status = f"巡检完成 · 发现 {issue_count} 项问题"

        return metrics, entries, status

    def build_page(self, metrics, config, entries, status):
        return {
            "title": "性能与诊断",
            "status": status,
            "header_actions": [
                {"id": "scan_now", "label": "立即巡检", "variant": "primary"},
            ],
            "cards": [
                {
                    "id": "workspace-metrics",
                    "title": "工作区体积与占用",
                    "items": [
                        {
                            "kind": "kv",
                            "rows": [
                                {"label": "Markdown 笔记总篇数", "value": f"{metrics['note_count']} 篇"},
                                {"label": "Markdown 总字符数", "value": f"{metrics['total_chars']:,} 字符"},
                                {"label": "Markdown 文本体积", "value": format_bytes(metrics["md_bytes"])},
                                {
                                    "label": "assets 附件数量与体积",
                                    "value": f"{metrics['asset_count']} 个 / {format_bytes(metrics['asset_bytes'])}",
                                },
                                {"label": "诊断进程内存占用 (RSS)", "value": format_bytes(metrics["rss_bytes"])},
                            ],
                        }
                    ],
                },
                {
                    "id": "health-checks",
                    "title": "健康巡检项",
                    "items": [
                        {
                            "kind": "switch",
                            "id": "check_orphan_assets",
                            "label": "检测未被引用的孤立附件",
                            "value": config["check_orphan_assets"],
                        },
                        {
                            "kind": "switch",
                            "id": "check_broken_links_and_empty",
                            "label": "检测失效相对链接与空笔记",
                            "value": config["check_broken_links_and_empty"],
                        },
                        {
                            "kind": "segmented",
                            "id": "large_asset_threshold",
                            "label": "大体积附件提醒阈值",
                            "value": config["large_asset_threshold"],
                            "options": [
                                {"value": "1mb", "label": "1 MB"},
                                {"value": "5mb", "label": "5 MB"},
                                {"value": "10mb", "label": "10 MB"},
                            ],
                        },
                        {
                            "kind": "actions",
                            "buttons": [
                                {"id": "save_config", "label": "保存配置", "variant": "primary"},
                            ],
                        },
                    ],
                },
                {
                    "id": "diagnostics-log",
                    "title": "诊断结果与巡检记录",
                    "items": [
                        {
                            "kind": "log_list",
                            "empty_text": "暂无巡检记录，点击右上角「立即巡检」开始检查",
                            "entries": entries,
                        }
                    ],
                },
            ],
        }

    def render(self, bridge):
        config, entries, status = self.load_state(bridge)
        metrics, _, _, _ = self.collect_workspace_data(bridge, read_contents=True)
        return {"page": self.build_page(metrics, config, entries, status)}

    def action(self, bridge, params):
        action_id = params.get("action_id") or ""
        values = params.get("values") or {}
        _, entries, status = self.load_state(bridge)
        config = normalize_config(values)

        if action_id == "scan_now":
            metrics, entries, status = self.run_scan(bridge, config)
            self.save_state(bridge, config, entries, status)
            page = self.build_page(metrics, config, entries, status)
            return {"values": config, "page": page, "status": status}

        if action_id == "save_config":
            status = "已保存巡检配置"
            self.save_state(bridge, config, entries, status)
            metrics, _, _, _ = self.collect_workspace_data(bridge, read_contents=True)
            page = self.build_page(metrics, config, entries, status)
            return {"values": config, "page": page, "status": status}

        self.save_state(bridge, config, entries, status)
        metrics, _, _, _ = self.collect_workspace_data(bridge, read_contents=True)
        page = self.build_page(metrics, config, entries, status)
        return {"values": config, "page": page, "status": status}


def normalize_config(raw):
    cfg = dict(DEFAULT_CONFIG)
    if isinstance(raw.get("check_orphan_assets"), bool):
        cfg["check_orphan_assets"] = raw["check_orphan_assets"]
    if isinstance(raw.get("check_broken_links_and_empty"), bool):
        cfg["check_broken_links_and_empty"] = raw["check_broken_links_and_empty"]
    threshold = raw.get("large_asset_threshold")
    if threshold in THRESHOLD_BYTES:
        cfg["large_asset_threshold"] = threshold
    return cfg


def normalize_entries(raw):
    entries = []
    if not isinstance(raw, list):
        return entries
    for item in raw:
        if not isinstance(item, dict):
            continue
        title = item.get("title")
        time_str = item.get("time")
        if isinstance(title, str) and title and isinstance(time_str, str) and time_str:
            entry = {"title": title, "time": time_str}
            status = item.get("status")
            if isinstance(status, str) and status:
                entry["status"] = status
            entries.append(entry)
    return entries[:MAX_LOG_ENTRIES]


def get_process_rss_bytes():
    maxrss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    if sys.platform == "darwin":
        return int(maxrss)
    return int(maxrss) * 1024


def format_bytes(size):
    size = max(0, int(size))
    if size < 1024:
        return f"{size} B"
    if size < 1024 * 1024:
        return f"{size / 1024:.1f} KB"
    if size < 1024 * 1024 * 1024:
        return f"{size / (1024 * 1024):.2f} MB"
    return f"{size / (1024 * 1024 * 1024):.2f} GB"


def normalize_rel_path(path):
    cleaned = path.replace("\\", "/").lstrip("/")
    return posixpath.normpath(cleaned)


def scan_assets(notebook_root):
    if not notebook_root:
        return []
    assets_dir = os.path.join(notebook_root, "assets")
    if not os.path.isdir(assets_dir):
        return []
    result = []
    for root, _, files in os.walk(assets_dir):
        for name in sorted(files):
            full_path = os.path.join(root, name)
            if not os.path.isfile(full_path):
                continue
            rel = os.path.relpath(full_path, notebook_root)
            try:
                size = os.path.getsize(full_path)
            except OSError:
                size = 0
            result.append({"rel_path": normalize_rel_path(rel), "size": size})
    return result


def strip_link_decorations(raw):
    s = raw.strip()
    if s.startswith("<") and ">" in s:
        s = s[1:s.index(">")].strip()
    else:
        parts = s.split()
        if parts:
            s = parts[0]
    s = s.split("#", 1)[0].split("?", 1)[0].strip()
    return unquote(s)


def is_external_or_anchor(target):
    if not target or target.startswith("#"):
        return True
    lower = target.lower()
    return (
        lower.startswith("http://")
        or lower.startswith("https://")
        or lower.startswith("mailto:")
        or lower.startswith("tel:")
        or lower.startswith("data:")
        or lower.startswith("javascript:")
    )


def extract_link_targets(content):
    targets = []
    for match in MD_LINK_RE.finditer(content):
        cleaned = strip_link_decorations(match.group(2))
        if not is_external_or_anchor(cleaned):
            targets.append((cleaned, False))
    for match in HTML_REF_RE.finditer(content):
        cleaned = strip_link_decorations(match.group(1))
        if not is_external_or_anchor(cleaned):
            targets.append((cleaned, False))
    for match in WIKI_LINK_RE.finditer(content):
        inner = match.group(2).split("|", 1)[0].split("#", 1)[0].strip()
        if inner and not is_external_or_anchor(inner):
            targets.append((unquote(inner), True))
    return targets


def resolve_relative_target(note_norm, raw_target, is_wiki):
    clean = raw_target.replace("\\", "/")
    if is_wiki:
        norm = posixpath.normpath(clean.lstrip("/"))
        return norm, posixpath.basename(norm)
    if clean.startswith("/"):
        norm = posixpath.normpath(clean.lstrip("/"))
    else:
        base_dir = posixpath.dirname(note_norm)
        norm = posixpath.normpath(posixpath.join(base_dir, clean)) if base_dir and base_dir != "." else posixpath.normpath(clean)
    return norm, None


def target_exists(notebook_root, note_norm, raw_target, is_wiki, note_paths_norm, assets):
    asset_paths = {a["rel_path"] for a in assets}
    asset_basenames = {posixpath.basename(a["rel_path"]) for a in assets}
    resolved, candidate_name = resolve_relative_target(note_norm, raw_target, is_wiki)

    if resolved in note_paths_norm or f"{resolved}.md" in note_paths_norm:
        return True
    if resolved in asset_paths:
        return True
    if is_wiki and candidate_name:
        if candidate_name in asset_basenames:
            return True
        if any(posixpath.basename(p) == candidate_name or posixpath.basename(p) == f"{candidate_name}.md" for p in note_paths_norm):
            return True

    if notebook_root and not resolved.startswith(".."):
        root_real = os.path.realpath(notebook_root)
        candidate_fs = os.path.realpath(os.path.join(root_real, resolved))
        if candidate_fs == root_real or candidate_fs.startswith(root_real + os.sep):
            if os.path.exists(candidate_fs):
                return True
            if not resolved.endswith(".md") and os.path.exists(candidate_fs + ".md"):
                return True
    return False


PLUGIN = DiagnosticsPlugin()


def handle_request(bridge, method, params):
    if method == "plugin.initialize":
        return PLUGIN.initialize(params)
    if method == "plugin.render":
        return PLUGIN.render(bridge)
    if method == "plugin.action":
        return PLUGIN.action(bridge, params)
    if method == "plugin.shutdown":
        return {"ok": True}
    raise RuntimeError(f"unsupported method: {method}")


if __name__ == "__main__":
    HostBridge().serve(handle_request)
