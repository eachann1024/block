#!/usr/bin/env python3
import csv
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
    {"value": "yuque", "label": "语雀目录"},
]
SOURCE_LABELS = {opt["value"]: opt["label"] for opt in SOURCE_OPTIONS}

ASSET_POLICY_OPTIONS = [
    {"value": "centralize", "label": "统一归集图片至 assets/"},
    {"value": "preserve", "label": "保留原相对目录"},
]
ASSET_POLICY_LABELS = {opt["value"]: opt["label"] for opt in ASSET_POLICY_OPTIONS}

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
    "obs_wikilinks": True,
    "obs_callouts": True,
    "notion_strip_id": True,
    "notion_csv_table": True,
    "yuque_clean_html": True,
    "asset_policy": "centralize",
    "conflict_mode": "skip",
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
        if not isinstance(raw, dict):
            return cfg
        if raw.get("source_type") in SOURCE_LABELS:
            cfg["source_type"] = raw["source_type"]
        if isinstance(raw.get("source_dir"), str):
            cfg["source_dir"] = raw["source_dir"].strip()
        if isinstance(raw.get("target_subdir"), str):
            sub = sanitize_subdir(raw["target_subdir"])
            cfg["target_subdir"] = sub or "imported"
        if isinstance(raw.get("obs_wikilinks"), bool):
            cfg["obs_wikilinks"] = raw["obs_wikilinks"]
        elif isinstance(raw.get("convert_syntax"), bool):
            cfg["obs_wikilinks"] = raw["convert_syntax"]
        if isinstance(raw.get("obs_callouts"), bool):
            cfg["obs_callouts"] = raw["obs_callouts"]
        elif isinstance(raw.get("convert_syntax"), bool):
            cfg["obs_callouts"] = raw["convert_syntax"]
        if isinstance(raw.get("notion_strip_id"), bool):
            cfg["notion_strip_id"] = raw["notion_strip_id"]
        if isinstance(raw.get("notion_csv_table"), bool):
            cfg["notion_csv_table"] = raw["notion_csv_table"]
        if isinstance(raw.get("yuque_clean_html"), bool):
            cfg["yuque_clean_html"] = raw["yuque_clean_html"]
        if raw.get("asset_policy") in ASSET_POLICY_LABELS:
            cfg["asset_policy"] = raw["asset_policy"]
        if raw.get("conflict_mode") in CONFLICT_LABELS:
            cfg["conflict_mode"] = raw["conflict_mode"]
        elif raw.get("conflict_policy") in CONFLICT_LABELS:
            cfg["conflict_mode"] = raw["conflict_policy"]
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
                {"id": "run_import", "label": "执行迁入", "variant": "primary"},
            ],
            "cards": [
                {
                    "id": "source-card",
                    "title": "迁移来源与目录",
                    "items": [
                        {
                            "kind": "segmented",
                            "id": "source_type",
                            "label": "来源类型",
                            "value": source_type,
                            "options": SOURCE_OPTIONS,
                        },
                        {
                            "kind": "folder",
                            "id": "source_dir",
                            "label": "源文件夹路径",
                            "value": cfg["source_dir"],
                            "placeholder": "选择或输入本地导出文件夹路径",
                        },
                        {
                            "kind": "input",
                            "id": "target_subdir",
                            "label": "迁入子目录",
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
                    "title": "语法清洗与附件策略",
                    "items": [
                        {
                            "kind": "switch",
                            "id": "obs_wikilinks",
                            "label": "转换 [[双链]] 与 ![[嵌入]] 为标准相对链接",
                            "value": bool(cfg["obs_wikilinks"]),
                            "visible_when": {"control_id": "source_type", "equals": "obsidian"},
                        },
                        {
                            "kind": "switch",
                            "id": "obs_callouts",
                            "label": "转换 Callout 与 ==高亮==",
                            "value": bool(cfg["obs_callouts"]),
                            "visible_when": {"control_id": "source_type", "equals": "obsidian"},
                        },
                        {
                            "kind": "switch",
                            "id": "notion_strip_id",
                            "label": "剥离文件与文件夹末尾 32 位随机 ID",
                            "value": bool(cfg["notion_strip_id"]),
                            "visible_when": {"control_id": "source_type", "equals": "notion"},
                        },
                        {
                            "kind": "switch",
                            "id": "notion_csv_table",
                            "label": "同名 CSV 数据库转为 Markdown 表格",
                            "value": bool(cfg["notion_csv_table"]),
                            "visible_when": {"control_id": "source_type", "equals": "notion"},
                        },
                        {
                            "kind": "switch",
                            "id": "yuque_clean_html",
                            "label": "清洗语雀 <a name> 锚点与 <br/> 标签",
                            "value": bool(cfg["yuque_clean_html"]),
                            "visible_when": {"control_id": "source_type", "equals": "yuque"},
                        },
                        {
                            "kind": "segmented",
                            "id": "asset_policy",
                            "label": "附件归集策略",
                            "value": cfg["asset_policy"],
                            "options": ASSET_POLICY_OPTIONS,
                        },
                        {
                            "kind": "segmented",
                            "id": "conflict_mode",
                            "label": "同名冲突处理",
                            "value": cfg["conflict_mode"],
                            "options": CONFLICT_OPTIONS,
                        },
                    ],
                },
                {
                    "id": "stats-card",
                    "title": "迁移概览与记录",
                    "items": [
                        {
                            "kind": "metrics",
                            "metrics": [
                                {"label": "来源类型", "value": SOURCE_LABELS[source_type]},
                                {"label": "待迁入篇数", "value": f"{stats['pending_count']} 篇"},
                                {"label": "累计已迁入篇数", "value": f"{stats['total_imported']} 篇"},
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
            return None, "请先选择或填写有效的源文件夹路径"
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

        md_files, asset_map, csv_map = collect_source_files(resolved_dir)
        stats["pending_count"] = len(md_files)
        bridge.call_host("host.storage.set", {"key": "stats", "value": stats})
        csv_note = f"、{len(csv_map['by_rel'])} 个 CSV" if cfg["source_type"] == "notion" and csv_map["by_rel"] else ""
        status = f"扫描完成：发现 {len(md_files)} 篇笔记与 {len(asset_map['by_rel'])} 个附件{csv_note}"
        page = self.build_page(cfg, stats, history, status_text=status)
        return {"values": cfg, "page": page, "status": status}

    def run_import(self, bridge, cfg, stats, history):
        resolved_dir, err = self.validate_source_dir(cfg["source_dir"])
        if err:
            stats["pending_count"] = 0
            bridge.call_host("host.storage.set", {"key": "stats", "value": stats})
            page = self.build_page(cfg, stats, history, status_text=err)
            return {"values": cfg, "page": page, "status": err}

        md_files, asset_map, csv_map = collect_source_files(resolved_dir)
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
        policy = cfg["conflict_mode"]

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

            strip_id = source_type == "notion" and bool(cfg["notion_strip_id"])
            norm_rel = normalize_rel_path(rel_md, strip_id=strip_id)
            target_rel = f"{subdir}/{norm_rel}".replace("\\", "/")

            if target_rel in existing_paths:
                if policy == "skip":
                    skipped_count += 1
                    continue
                if policy == "keep_both":
                    target_rel = allocate_unique_path(target_rel, existing_paths)

            cleaned = transform_markdown(
                raw_content,
                cfg=cfg,
                abs_md=abs_md,
                rel_md=rel_md,
                source_root=resolved_dir,
                asset_map=asset_map,
                csv_map=csv_map,
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
    csv_by_rel = {}
    csv_by_dir_stem = {}
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
            elif ext == ".csv":
                csv_by_rel[rel_path] = abs_path
                rel_dir = os.path.dirname(rel_path)
                stem = os.path.splitext(fname)[0]
                csv_by_dir_stem[(rel_dir, stem)] = abs_path
                stripped_stem = strip_notion_hash(stem)
                if stripped_stem:
                    csv_by_dir_stem.setdefault((rel_dir, stripped_stem), abs_path)
            elif ext in ASSET_EXTS:
                by_rel[rel_path] = abs_path
                by_name.setdefault(fname, abs_path)
    return md_files, {"by_rel": by_rel, "by_name": by_name}, {"by_rel": csv_by_rel, "by_dir_stem": csv_by_dir_stem}


def strip_notion_hash(segment):
    return re.sub(r'\s+[0-9a-fA-F]{32}$', '', segment).strip()


def normalize_rel_path(rel_path, strip_id=False):
    parts = rel_path.replace("\\", "/").split("/")
    out = []
    for idx, seg in enumerate(parts):
        is_last = idx == len(parts) - 1
        if is_last:
            stem, _ = os.path.splitext(seg)
            if strip_id:
                stem = strip_notion_hash(stem) or stem
            out.append(stem + ".md")
        else:
            cleaned_dir = strip_notion_hash(seg) if strip_id else seg
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


def copy_asset_to_notebook(src_asset, source_root, notebook_root, target_subdir, target_note_rel, asset_policy, strip_id, copied_assets):
    if not notebook_root:
        return os.path.basename(src_asset)
    nb_real = os.path.realpath(notebook_root)
    src_real = os.path.realpath(src_asset)
    root_real = os.path.realpath(source_root)

    if asset_policy == "preserve" and src_real.startswith(root_real + os.sep):
        rel_asset = os.path.relpath(src_real, root_real).replace("\\", "/")
        if strip_id:
            parts = rel_asset.split("/")
            cleaned_dirs = [strip_notion_hash(d) or d for d in parts[:-1]]
            rel_asset = "/".join(cleaned_dirs + [parts[-1]])
        dst_path = os.path.join(nb_real, target_subdir, rel_asset)
    else:
        assets_dir = os.path.join(nb_real, target_subdir, "assets")
        fname = os.path.basename(src_real)
        dst_path = os.path.join(assets_dir, fname)

    os.makedirs(os.path.dirname(dst_path), exist_ok=True)
    if dst_path not in copied_assets:
        shutil.copy2(src_real, dst_path)
        copied_assets.add(dst_path)
    note_abs_dir = os.path.dirname(os.path.join(nb_real, target_note_rel))
    return os.path.relpath(dst_path, note_abs_dir).replace("\\", "/")


def csv_file_to_markdown_table(csv_path, max_rows=200):
    try:
        with open(csv_path, "r", encoding="utf-8-sig", errors="replace", newline="") as f:
            reader = list(csv.reader(f))
    except OSError:
        return ""
    if not reader:
        return ""
    headers = reader[0]
    if not headers:
        return ""
    col_count = len(headers)

    def fmt_cell(val):
        return (val or "").replace("\r\n", " ").replace("\n", " ").replace("|", "\\|").strip()

    lines = [
        "| " + " | ".join(fmt_cell(h) or "-" for h in headers) + " |",
        "| " + " | ".join("---" for _ in headers) + " |",
    ]
    for row in reader[1:max_rows + 1]:
        padded = list(row[:col_count]) + [""] * max(0, col_count - len(row))
        lines.append("| " + " | ".join(fmt_cell(c) for c in padded) + " |")
    if len(reader) - 1 > max_rows:
        lines.append("")
        lines.append(f"注：原 CSV 共 {len(reader) - 1} 行，已展示前 {max_rows} 行。")
    return "\n".join(lines)


def transform_markdown(
    content,
    cfg,
    abs_md,
    rel_md,
    source_root,
    asset_map,
    csv_map,
    notebook_root,
    target_subdir,
    target_note_rel,
    copied_assets,
):
    source_type = cfg["source_type"]
    obs_wikilinks = source_type == "obsidian" and bool(cfg["obs_wikilinks"])
    obs_callouts = source_type == "obsidian" and bool(cfg["obs_callouts"])
    notion_strip_id = source_type == "notion" and bool(cfg["notion_strip_id"])
    notion_csv_table = source_type == "notion" and bool(cfg["notion_csv_table"])
    yuque_clean_html = source_type == "yuque" and bool(cfg["yuque_clean_html"])
    asset_policy = cfg["asset_policy"]

    lines = content.replace("\r\n", "\n").splitlines()
    out_lines = []
    in_fence = False
    fence_marker = ""
    embedded_csv_paths = set()

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

        if yuque_clean_html:
            processed = re.sub(r'<a\b[^>]*(?:name|id)=["\'][^"\']*["\'][^>]*>\s*</a>', '', processed, flags=re.I)
            processed = re.sub(r'<a\b[^>]*(?:name|id)=["\'][^"\']*["\'][^>]*/\s*>', '', processed, flags=re.I)
            processed = re.sub(r'<br\s*/?>', ' ' if "|" in processed else '  \n', processed, flags=re.I)

        if obs_callouts:
            m_callout = re.match(r'^(\s*>\s*)\[!([A-Za-z0-9_-]+)\][+-]?\s*(.*)$', processed)
            if m_callout:
                prefix, ctype, ctitle = m_callout.group(1), m_callout.group(2).upper(), m_callout.group(3).strip()
                processed = f"{prefix}**{ctype}：{ctitle}**" if ctitle else f"{prefix}**{ctype}**"

        if source_type == "notion" and notion_strip_id:
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
                rel_link = copy_asset_to_notebook(
                    src_asset, source_root, notebook_root, target_subdir, target_note_rel, asset_policy, notion_strip_id, copied_assets
                )
                return f"![{alt}]({rel_link})"
            return m.group(0)

        processed = re.sub(r'!\[([^\]]*)\]\(([^)]+)\)', repl_md_image, processed)

        if obs_wikilinks:
            def repl_embed(m):
                inner = m.group(1).strip()
                target_name = inner.split("|")[0].strip()
                src_asset = resolve_asset_source(target_name, abs_md, source_root, asset_map)
                if src_asset:
                    rel_link = copy_asset_to_notebook(
                        src_asset, source_root, notebook_root, target_subdir, target_note_rel, asset_policy, False, copied_assets
                    )
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

        if obs_callouts:
            processed = re.sub(r'==([^=\n]+)==', r'<mark>\1</mark>', processed)

        if notion_csv_table:
            for m_csv in re.finditer(r'(?<!!)\[([^\]]+)\]\(([^)]+\.csv)\)', processed, re.I):
                csv_rel = unquote(m_csv.group(2).strip())
                csv_abs = os.path.realpath(os.path.join(os.path.dirname(abs_md), csv_rel))
                root_real = os.path.realpath(source_root)
                if csv_abs.startswith(root_real + os.sep) and os.path.isfile(csv_abs):
                    embedded_csv_paths.add(csv_abs)

        out_lines.append(processed)

    if notion_csv_table:
        rel_dir = os.path.dirname(rel_md.replace("\\", "/"))
        md_stem = os.path.splitext(os.path.basename(rel_md))[0]
        stripped_stem = strip_notion_hash(md_stem)
        for key in ((rel_dir, md_stem), (rel_dir, stripped_stem)):
            matched_csv = csv_map["by_dir_stem"].get(key)
            if matched_csv:
                embedded_csv_paths.add(os.path.realpath(matched_csv))
        for csv_abs in sorted(embedded_csv_paths):
            table_md = csv_file_to_markdown_table(csv_abs)
            if table_md:
                csv_title = strip_notion_hash(os.path.splitext(os.path.basename(csv_abs))[0]) or os.path.basename(csv_abs)
                out_lines.append("")
                out_lines.append(f"## {csv_title}")
                out_lines.append("")
                out_lines.append(table_md)

    return "\n".join(out_lines) + ("\n" if content.endswith("\n") else "")


def main():
    bridge = HostBridge()
    plugin = MigratorPlugin()
    bridge.serve(plugin.handle)


if __name__ == "__main__":
    main()
