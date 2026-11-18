#!/usr/bin/env python3
import json
import os
import re
import shutil
import sys
from datetime import datetime
from urllib.parse import unquote


SOURCE_OPTIONS = [
    {"value": "obsidian", "label": "Obsidian 库"},
    {"value": "notion", "label": "Notion 导出目录"},
    {"value": "yuque", "label": "语雀导出目录"},
]
SOURCE_LABELS = {opt["value"]: opt["label"] for opt in SOURCE_OPTIONS}

CONFLICT_OPTIONS = [
    {"value": "skip", "label": "跳过已有"},
    {"value": "keep_both", "label": "保留双份"},
    {"value": "overwrite", "label": "覆盖更新"},
]
CONFLICT_LABELS = {opt["value"]: opt["label"] for opt in CONFLICT_OPTIONS}

SKIP_DIRS = {".git", ".obsidian", ".trash", ".DS_Store", "node_modules", "__pycache__"}
ASSET_EXTS = {".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".pdf", ".mp3", ".mp4", ".wav", ".zip"}

DEFAULT_CONFIG = {
    "source_type": "obsidian",
    "source_dir": "",
    "target_subdir": "imported",
    "convert_syntax": True,
    "conflict_policy": "skip",
}

DEFAULT_STATS = {
    "pending_count": 0,
    "total_imported": 0,
}


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
            line = line.strip()
            if not line:
                continue
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


class MigratorPlugin:
    def __init__(self):
        self.data_dir = ""
        self.notebook_root = ""

    def sanitize_config(self, raw):
        cfg = dict(DEFAULT_CONFIG)
        if isinstance(raw, dict):
            if raw.get("source_type") in SOURCE_LABELS:
                cfg["source_type"] = raw["source_type"]
            if isinstance(raw.get("source_dir"), str):
                cfg["source_dir"] = raw["source_dir"].strip()
            if isinstance(raw.get("target_subdir"), str):
                sub = sanitize_subdir(raw["target_subdir"])
                cfg["target_subdir"] = sub or "imported"
            if isinstance(raw.get("convert_syntax"), bool):
                cfg["convert_syntax"] = raw["convert_syntax"]
            if raw.get("conflict_policy") in CONFLICT_LABELS:
                cfg["conflict_policy"] = raw["conflict_policy"]
        return cfg

    def load_state(self, bridge):
        cfg_res = bridge.call_host("host.storage.get", {"key": "config"})
        stats_res = bridge.call_host("host.storage.get", {"key": "stats"})
        hist_res = bridge.call_host("host.storage.get", {"key": "history"})

        cfg = self.sanitize_config(cfg_res.get("value") if cfg_res.get("found") else {})
        stats = dict(DEFAULT_STATS)
        if stats_res.get("found") and isinstance(stats_res.get("value"), dict):
            val = stats_res["value"]
            if isinstance(val.get("pending_count"), int):
                stats["pending_count"] = val["pending_count"]
            if isinstance(val.get("total_imported"), int):
                stats["total_imported"] = val["total_imported"]

        history = []
        if hist_res.get("found") and isinstance(hist_res.get("value"), list):
            for entry in hist_res["value"][:20]:
                if isinstance(entry, dict) and entry.get("title") and entry.get("time"):
                    item = {"title": str(entry["title"]), "time": str(entry["time"])}
                    if entry.get("status"):
                        item["status"] = str(entry["status"])
                    history.append(item)
        return cfg, stats, history

    def build_page(self, cfg, stats, history, status_text=None):
        source_type = cfg["source_type"]
        return {
            "title": "笔记迁移",
            "status": status_text or f"{SOURCE_LABELS[source_type]} · 已就绪",
            "header_actions": [
                {"id": "run_import", "label": "执行迁入", "variant": "primary"}
            ],
            "cards": [
                {
                    "id": "source-card",
                    "title": "迁移来源",
                    "items": [
                        {
                            "kind": "segmented",
                            "id": "source_type",
                            "label": "来源类型",
                            "value": source_type,
                            "options": SOURCE_OPTIONS,
                        },
                        {
                            "kind": "input",
                            "id": "source_dir",
                            "label": "源目录路径",
                            "value": cfg["source_dir"],
                            "placeholder": "请输入本地导出文件夹绝对路径",
                        },
                        {
                            "kind": "input",
                            "id": "target_subdir",
                            "label": "迁入目标子目录",
                            "value": cfg["target_subdir"],
                            "placeholder": "默认 imported",
                        },
                        {
                            "kind": "actions",
                            "buttons": [
                                {"id": "save_config", "label": "保存配置", "variant": "secondary"},
                                {"id": "scan_preview", "label": "扫描预览", "variant": "primary"},
                            ],
                        },
                    ],
                },
                {
                    "id": "rules-card",
                    "title": "语法清洗规则",
                    "items": [
                        {
                            "kind": "switch",
                            "id": "convert_syntax",
                            "label": "转换双链与高亮语法为标准 Markdown",
                            "value": bool(cfg["convert_syntax"]),
                        },
                        {
                            "kind": "segmented",
                            "id": "conflict_policy",
                            "label": "同名处理",
                            "value": cfg["conflict_policy"],
                            "options": CONFLICT_OPTIONS,
                        },
                    ],
                },
                {
                    "id": "stats-card",
                    "title": "迁移统计与记录",
                    "items": [
                        {
                            "kind": "kv",
                            "rows": [
                                {"label": "来源类型", "value": SOURCE_LABELS[source_type]},
                                {"label": "待迁入篇数", "value": f"{stats['pending_count']} 篇"},
                                {"label": "累计迁入篇数", "value": f"{stats['total_imported']} 篇"},
                            ],
                        },
                        {
                            "kind": "log_list",
                            "empty_text": "暂无迁移记录",
                            "entries": history,
                        },
                    ],
                },
            ],
        }

    def handle(self, bridge, method, params):
        if method == "plugin.initialize":
            self.data_dir = params.get("data_dir") or ""
            self.notebook_root = params.get("notebook_root") or ""
            return {"ready": True}

        if method == "plugin.render":
            cfg, stats, history = self.load_state(bridge)
            return {"page": self.build_page(cfg, stats, history)}

        if method == "plugin.action":
            action_id = params.get("action_id") or ""
            values = params.get("values") or {}
            cfg, stats, history = self.load_state(bridge)
            merged = dict(cfg)
            merged.update(values)
            cfg = self.sanitize_config(merged)
            bridge.call_host("host.storage.set", {"key": "config", "value": cfg})

            if action_id == "save_config":
                status = "配置已保存"
                page = self.build_page(cfg, stats, history, status_text=status)
                return {"values": cfg, "page": page, "status": status}

            if action_id == "scan_preview":
                return self.scan_preview(bridge, cfg, stats, history)

            if action_id == "run_import":
                return self.run_import(bridge, cfg, stats, history)

            page = self.build_page(cfg, stats, history)
            return {"values": cfg, "page": page}

        if method == "plugin.shutdown":
            return {"ok": True}

        return {}

    def validate_source_dir(self, source_dir):
        raw = (source_dir or "").strip()
        if not raw:
            return None, "请先填写有效的源目录路径"
        resolved = os.path.abspath(os.path.expanduser(raw))
        if not os.path.isdir(resolved):
            return None, "源目录不存在或不可访问，请检查路径"
        return resolved, ""

    def scan_preview(self, bridge, cfg, stats, history):
        resolved_dir, err = self.validate_source_dir(cfg["source_dir"])
        if err:
            stats["pending_count"] = 0
            bridge.call_host("host.storage.set", {"key": "stats", "value": stats})
            page = self.build_page(cfg, stats, history, status_text=err)
            return {"values": cfg, "page": page, "status": err}

        md_files, asset_map = collect_source_files(resolved_dir)
        stats["pending_count"] = len(md_files)
        bridge.call_host("host.storage.set", {"key": "stats", "value": stats})
        status = f"扫描完成：发现 {len(md_files)} 篇笔记与 {len(asset_map['by_rel'])} 个附件"
        page = self.build_page(cfg, stats, history, status_text=status)
        return {"values": cfg, "page": page, "status": status}

    def run_import(self, bridge, cfg, stats, history):
        resolved_dir, err = self.validate_source_dir(cfg["source_dir"])
        if err:
            stats["pending_count"] = 0
            bridge.call_host("host.storage.set", {"key": "stats", "value": stats})
            page = self.build_page(cfg, stats, history, status_text=err)
            return {"values": cfg, "page": page, "status": err}

        md_files, asset_map = collect_source_files(resolved_dir)
        stats["pending_count"] = len(md_files)
        if not md_files:
            bridge.call_host("host.storage.set", {"key": "stats", "value": stats})
            status = "源目录中未找到 Markdown 笔记"
            page = self.build_page(cfg, stats, history, status_text=status)
            return {"values": cfg, "page": page, "status": status}

        existing_notes = bridge.call_host("host.notes.list", {}).get("notes") or []
        existing_paths = {n.get("path", "") for n in existing_notes if n.get("path")}
        subdir = sanitize_subdir(cfg["target_subdir"]) or "imported"
        source_type = cfg["source_type"]
        policy = cfg["conflict_policy"]
        convert = bool(cfg["convert_syntax"])

        imported_count = 0
        skipped_count = 0
        copied_assets = set()
        now_str = datetime.now().strftime("%H:%M:%S")
        new_logs = []

        for abs_md, rel_md in md_files:
            try:
                with open(abs_md, "r", encoding="utf-8", errors="replace") as f:
                    raw_content = f.read()
            except OSError:
                skipped_count += 1
                continue

            norm_rel = normalize_rel_path(rel_md, source_type)
            target_rel = f"{subdir}/{norm_rel}".replace("\\", "/")

            if target_rel in existing_paths:
                if policy == "skip":
                    skipped_count += 1
                    continue
                if policy == "keep_both":
                    target_rel = allocate_unique_path(target_rel, existing_paths)

            cleaned = transform_markdown(
                raw_content,
                source_type=source_type,
                convert_syntax=convert,
                abs_md=abs_md,
                source_root=resolved_dir,
                asset_map=asset_map,
                notebook_root=self.notebook_root,
                target_subdir=subdir,
                target_note_rel=target_rel,
                copied_assets=copied_assets,
            )

            try:
                bridge.call_host("host.notes.write", {"path": target_rel, "content": cleaned})
            except RuntimeError as exc:
                new_logs.append({"title": os.path.basename(target_rel), "time": now_str, "status": f"写入失败: {exc}"})
                continue

            existing_paths.add(target_rel)
            imported_count += 1
            new_logs.append({"title": target_rel, "time": now_str, "status": "已迁入"})

        stats["total_imported"] += imported_count
        stats["pending_count"] = max(0, len(md_files) - imported_count)
        bridge.call_host("host.storage.set", {"key": "stats", "value": stats})

        summary_entry = {
            "title": f"{SOURCE_LABELS[source_type]}迁入 ({imported_count} 篇, 附件 {len(copied_assets)} 个)",
            "time": now_str,
            "status": f"跳过 {skipped_count} 篇" if skipped_count else "已完成",
        }
        history = ([summary_entry] + new_logs[:10] + history)[:20]
        bridge.call_host("host.storage.set", {"key": "history", "value": history})

        status = f"迁入完成：成功 {imported_count} 篇，拷贝附件 {len(copied_assets)} 个"
        if skipped_count:
            status += f"，跳过 {skipped_count} 篇"
        page = self.build_page(cfg, stats, history, status_text=status)
        return {"values": cfg, "page": page, "status": status}


def sanitize_subdir(raw):
    parts = []
    for seg in (raw or "").replace("\\", "/").split("/"):
        seg = seg.strip()
        if not seg or seg in (".", ".."):
            continue
        parts.append(seg)
    return "/".join(parts)


def collect_source_files(source_root):
    md_files = []
    by_rel = {}
    by_name = {}
    root_real = os.path.realpath(source_root)
    for dirpath, dirnames, filenames in os.walk(root_real):
        dirnames[:] = [d for d in sorted(dirnames) if d not in SKIP_DIRS and not d.startswith(".")]
        for fname in sorted(filenames):
            if fname.startswith(".") or fname == ".DS_Store":
                continue
            abs_path = os.path.join(dirpath, fname)
            rel_path = os.path.relpath(abs_path, root_real).replace("\\", "/")
            ext = os.path.splitext(fname)[1].lower()
            if ext in (".md", ".markdown"):
                md_files.append((abs_path, rel_path))
            elif ext in ASSET_EXTS:
                by_rel[rel_path] = abs_path
                by_name.setdefault(fname, abs_path)
    return md_files, {"by_rel": by_rel, "by_name": by_name}


def strip_notion_hash(segment):
    return re.sub(r'\s+[0-9a-fA-F]{32}$', '', segment).strip()


def normalize_rel_path(rel_path, source_type):
    parts = rel_path.replace("\\", "/").split("/")
    out = []
    for idx, seg in enumerate(parts):
        is_last = idx == len(parts) - 1
        if is_last:
            stem, _ = os.path.splitext(seg)
            if source_type == "notion":
                stem = strip_notion_hash(stem) or stem
            out.append(stem + ".md")
        else:
            cleaned_dir = strip_notion_hash(seg) if source_type == "notion" else seg
            out.append(cleaned_dir or seg)
    return "/".join(out)


def allocate_unique_path(target_rel, existing_paths):
    stem, ext = os.path.splitext(target_rel)
    idx = 1
    while True:
        candidate = f"{stem}_{idx}{ext}"
        if candidate not in existing_paths:
            return candidate
        idx += 1


def resolve_asset_source(ref, abs_md, source_root, asset_map):
    clean = unquote(ref.strip().split("#")[0].split("|")[0]).strip()
    if not clean or re.match(r'^(https?:|data:|mailto:)', clean, re.I):
        return None
    md_dir = os.path.dirname(abs_md)
    root_real = os.path.realpath(source_root)
    candidate = os.path.realpath(os.path.join(md_dir, clean))
    if candidate.startswith(root_real + os.sep) and os.path.isfile(candidate):
        ext = os.path.splitext(candidate)[1].lower()
        if ext in ASSET_EXTS:
            return candidate
    rel_key = clean.lstrip("./")
    if rel_key in asset_map["by_rel"]:
        return asset_map["by_rel"][rel_key]
    base = os.path.basename(clean)
    return asset_map["by_name"].get(base)


def copy_asset_to_notebook(src_asset, notebook_root, target_subdir, target_note_rel, copied_assets):
    if not notebook_root:
        return os.path.basename(src_asset)
    nb_real = os.path.realpath(notebook_root)
    assets_dir = os.path.join(nb_real, target_subdir, "assets")
    os.makedirs(assets_dir, exist_ok=True)
    fname = os.path.basename(src_asset)
    dst_path = os.path.join(assets_dir, fname)
    if dst_path not in copied_assets:
        shutil.copy2(src_asset, dst_path)
        copied_assets.add(dst_path)
    note_abs_dir = os.path.dirname(os.path.join(nb_real, target_note_rel))
    return os.path.relpath(dst_path, note_abs_dir).replace("\\", "/")


def transform_markdown(
    content,
    source_type,
    convert_syntax,
    abs_md,
    source_root,
    asset_map,
    notebook_root,
    target_subdir,
    target_note_rel,
    copied_assets,
):
    lines = content.replace("\r\n", "\n").splitlines()
    out_lines = []
    in_fence = False
    fence_marker = ""

    for line in lines:
        stripped = line.strip()
        m_fence = re.match(r'^(`{3,}|~{3,})', stripped)
        if m_fence:
            marker = m_fence.group(1)[0]
            if not in_fence:
                in_fence = True
                fence_marker = marker
            elif stripped.startswith(fence_marker * 3):
                in_fence = False
                fence_marker = ""
            out_lines.append(line)
            continue

        if in_fence:
            out_lines.append(line)
            continue

        processed = line

        if source_type == "yuque":
            processed = re.sub(r'<a\s+(?:name|id)=["\'][^"\']*["\']\s*>\s*</a>', '', processed, flags=re.I)
            processed = re.sub(r'<br\s*/?>', '  \n' if "|" not in processed else ' ', processed, flags=re.I)

        if source_type == "notion" and convert_syntax:
            def repl_notion_link(m):
                label, target = m.group(1), m.group(2)
                if re.match(r'^(https?:|data:|mailto:)', target.strip(), re.I):
                    return m.group(0)
                decoded = unquote(target)
                parts = decoded.split("/")
                new_parts = []
                for idx, seg in enumerate(parts):
                    if idx == len(parts) - 1:
                        s, ext = os.path.splitext(seg)
                        if ext.lower() in (".md", ".markdown"):
                            seg = (strip_notion_hash(s) or s) + ".md"
                    else:
                        seg = strip_notion_hash(seg) or seg
                    new_parts.append(seg)
                return f"[{label}]({'/'.join(new_parts)})"

            processed = re.sub(r'(?<!!)\[([^\]]+)\]\(([^)]+)\)', repl_notion_link, processed)

        def repl_md_image(m):
            alt, target = m.group(1), m.group(2)
            src_asset = resolve_asset_source(target, abs_md, source_root, asset_map)
            if src_asset:
                rel_link = copy_asset_to_notebook(src_asset, notebook_root, target_subdir, target_note_rel, copied_assets)
                return f"![{alt}]({rel_link})"
            return m.group(0)

        processed = re.sub(r'!\[([^\]]*)\]\(([^)]+)\)', repl_md_image, processed)

        if convert_syntax:
            def repl_embed(m):
                inner = m.group(1).strip()
                target_name = inner.split("|")[0].strip()
                src_asset = resolve_asset_source(target_name, abs_md, source_root, asset_map)
                if src_asset:
                    rel_link = copy_asset_to_notebook(src_asset, notebook_root, target_subdir, target_note_rel, copied_assets)
                    alt = os.path.splitext(os.path.basename(target_name))[0]
                    return f"![{alt}]({rel_link})"
                return f"[{target_name}]({target_name}.md)"

            processed = re.sub(r'!\[\[([^\]]+)\]\]', repl_embed, processed)

            def repl_wikilink(m):
                inner = m.group(1).strip()
                if "|" in inner:
                    target, alias = inner.split("|", 1)
                else:
                    target, alias = inner, inner
                target = target.strip()
                alias = alias.strip()
                if "#" in target:
                    note_part, anchor = target.split("#", 1)
                    note_part = note_part.strip()
                    slug = re.sub(r'\s+', '-', anchor.strip().lower())
                    href = f"{note_part}.md#{slug}" if note_part else f"#{slug}"
                else:
                    href = f"{target}.md"
                return f"[{alias}]({href})"

            processed = re.sub(r'\[\[([^\]]+)\]\]', repl_wikilink, processed)
            processed = re.sub(r'==([^=\n]+)==', r'<mark>\1</mark>', processed)

        out_lines.append(processed)

    return "\n".join(out_lines) + ("\n" if content.endswith("\n") else "")


def main():
    bridge = HostBridge()
    plugin = MigratorPlugin()
    bridge.serve(plugin.handle)


if __name__ == "__main__":
    main()
