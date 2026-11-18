#!/usr/bin/env python3
import html
import json
import os
import re
import shutil
import sys
from datetime import datetime


CHANNEL_OPTIONS = [
    {"value": "static_site", "label": "静态博客站点"},
    {"value": "wechat_mp", "label": "微信公众号排版 HTML"},
    {"value": "xiaohongshu", "label": "小红书图文分段稿"},
]
CHANNEL_LABELS = {opt["value"]: opt["label"] for opt in CHANNEL_OPTIONS}

THEME_OPTIONS = [
    {"value": "ocean_paper", "label": "海盐纸白"},
    {"value": "ink_minimal", "label": "墨夜极简"},
    {"value": "serif_magazine", "label": "杂志衬线"},
]
THEME_LABELS = {opt["value"]: opt["label"] for opt in THEME_OPTIONS}

THEMES = {
    "ocean_paper": {
        "bg": "#F7F9FB",
        "surface": "#FFFFFF",
        "text": "#1E293B",
        "muted": "#64748B",
        "accent": "#2563EB",
        "accent_soft": "#EFF6FF",
        "border": "#E2E8F0",
        "code_bg": "#F1F5F9",
        "font": "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Helvetica Neue', sans-serif",
    },
    "ink_minimal": {
        "bg": "#111317",
        "surface": "#181B20",
        "text": "#E5E7EB",
        "muted": "#9CA3AF",
        "accent": "#60A5FA",
        "accent_soft": "#1E293B",
        "border": "#2A2F38",
        "code_bg": "#0D0F12",
        "font": "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Helvetica Neue', sans-serif",
    },
    "serif_magazine": {
        "bg": "#FAF7F2",
        "surface": "#FFFDF9",
        "text": "#2B241D",
        "muted": "#7A6F64",
        "accent": "#9A3412",
        "accent_soft": "#FEF3C7",
        "border": "#E7DFD3",
        "code_bg": "#F3ECE1",
        "font": "'Songti SC', 'Noto Serif SC', Georgia, serif",
    },
}

DEFAULT_CONFIG = {
    "channel": "static_site",
    "output_dir": "",
    "site_title": "Block 数字花园",
    "theme": "ocean_paper",
    "generate_index": True,
}

DEFAULT_STATS = {
    "last_count": 0,
    "last_output": "",
    "last_channel": "static_site",
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


class PublisherPlugin:
    def __init__(self):
        self.data_dir = ""
        self.notebook_root = ""

    def sanitize_config(self, raw):
        cfg = dict(DEFAULT_CONFIG)
        if isinstance(raw, dict):
            if raw.get("channel") in CHANNEL_LABELS:
                cfg["channel"] = raw["channel"]
            if isinstance(raw.get("output_dir"), str):
                cfg["output_dir"] = raw["output_dir"].strip()
            if isinstance(raw.get("site_title"), str) and raw["site_title"].strip():
                cfg["site_title"] = raw["site_title"].strip()
            elif isinstance(raw.get("site_title"), str):
                cfg["site_title"] = ""
            if raw.get("theme") in THEME_LABELS:
                cfg["theme"] = raw["theme"]
            if isinstance(raw.get("generate_index"), bool):
                cfg["generate_index"] = raw["generate_index"]
        return cfg

    def load_state(self, bridge):
        cfg_res = bridge.call_host("host.storage.get", {"key": "config"})
        stats_res = bridge.call_host("host.storage.get", {"key": "stats"})
        hist_res = bridge.call_host("host.storage.get", {"key": "history"})

        cfg = self.sanitize_config(cfg_res.get("value") if cfg_res.get("found") else {})
        stats = dict(DEFAULT_STATS)
        if stats_res.get("found") and isinstance(stats_res.get("value"), dict):
            val = stats_res["value"]
            if isinstance(val.get("last_count"), int):
                stats["last_count"] = val["last_count"]
            if isinstance(val.get("last_output"), str):
                stats["last_output"] = val["last_output"]
            if val.get("last_channel") in CHANNEL_LABELS:
                stats["last_channel"] = val["last_channel"]

        history = []
        if hist_res.get("found") and isinstance(hist_res.get("value"), list):
            for entry in hist_res["value"][:20]:
                if isinstance(entry, dict) and entry.get("title") and entry.get("time"):
                    item = {"title": str(entry["title"]), "time": str(entry["time"])}
                    if entry.get("status"):
                        item["status"] = str(entry["status"])
                    history.append(item)
        return cfg, stats, history

    def resolve_output_dir(self, output_dir):
        raw = (output_dir or "").strip()
        if not raw:
            base = self.data_dir or os.getcwd()
            return os.path.abspath(os.path.join(base, "dist"))
        expanded = os.path.expanduser(raw)
        if os.path.isabs(expanded):
            return os.path.abspath(expanded)
        base = self.data_dir or os.getcwd()
        return os.path.abspath(os.path.join(base, expanded))

    def build_page(self, cfg, stats, history, status_text=None):
        channel = cfg["channel"]
        out_path = stats.get("last_output") or self.resolve_output_dir(cfg.get("output_dir"))
        if not out_path:
            out_path = "data_dir/dist"
        return {
            "title": "发布与导出",
            "status": status_text or f"{CHANNEL_LABELS[channel]} · 已就绪",
            "header_actions": [
                {"id": "build_now", "label": "一键构建导出", "variant": "primary"}
            ],
            "cards": [
                {
                    "id": "channel-card",
                    "title": "导出渠道与目录",
                    "items": [
                        {
                            "kind": "segmented",
                            "id": "channel",
                            "label": "导出渠道",
                            "value": channel,
                            "options": CHANNEL_OPTIONS,
                        },
                        {
                            "kind": "input",
                            "id": "output_dir",
                            "label": "输出目录",
                            "value": cfg["output_dir"],
                            "placeholder": "留空默认输出到插件数据目录下的 dist 文件夹",
                        },
                        {
                            "kind": "input",
                            "id": "site_title",
                            "label": "站点或专栏标题",
                            "value": cfg["site_title"],
                            "placeholder": "例如：Block 数字花园",
                        },
                        {
                            "kind": "actions",
                            "buttons": [
                                {"id": "save_config", "label": "保存配置", "variant": "primary"}
                            ],
                        },
                    ],
                },
                {
                    "id": "layout-card",
                    "title": "排版与内容选项",
                    "items": [
                        {
                            "kind": "segmented",
                            "id": "theme",
                            "label": "排版主题",
                            "value": cfg["theme"],
                            "options": THEME_OPTIONS,
                        },
                        {
                            "kind": "switch",
                            "id": "generate_index",
                            "label": "自动生成目录索引页",
                            "value": bool(cfg["generate_index"]),
                        },
                    ],
                },
                {
                    "id": "overview-card",
                    "title": "导出概览与历史",
                    "items": [
                        {
                            "kind": "kv",
                            "rows": [
                                {"label": "当前渠道", "value": CHANNEL_LABELS[channel]},
                                {"label": "最近构建篇数", "value": f"{stats['last_count']} 篇"},
                                {"label": "输出路径", "value": out_path},
                            ],
                        },
                        {
                            "kind": "log_list",
                            "empty_text": "暂无发布记录",
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

            if action_id == "build_now":
                return self.run_build(bridge, cfg, stats, history)

            page = self.build_page(cfg, stats, history)
            return {"values": cfg, "page": page}

        if method == "plugin.shutdown":
            return {"ok": True}

        return {}

    def run_build(self, bridge, cfg, stats, history):
        out_dir = self.resolve_output_dir(cfg["output_dir"])
        os.makedirs(out_dir, exist_ok=True)

        notes_meta = bridge.call_host("host.notes.list", {}).get("notes") or []
        site_title = cfg["site_title"] or "Block 数字花园"
        theme = THEMES[cfg["theme"]]
        channel = cfg["channel"]

        exported = []
        for meta in notes_meta:
            rel_path = meta.get("path") or ""
            if not rel_path:
                continue
            read_res = bridge.call_host("host.notes.read", {"path": rel_path})
            content = read_res.get("content") or ""
            title, body_md, excerpt = extract_meta_and_body(rel_path, content)
            self.copy_note_assets(rel_path, body_md, out_dir)

            if channel == "static_site":
                target_rel = change_ext(rel_path, ".html")
                html_doc = render_static_article(title, body_md, site_title, theme, rel_path)
                write_text_file(os.path.join(out_dir, target_rel), html_doc)
                exported.append({"title": title, "rel": target_rel, "excerpt": excerpt, "source": rel_path, "extra": ""})
            elif channel == "wechat_mp":
                target_rel = change_ext(rel_path, ".wechat.html")
                html_doc = render_wechat_article(title, body_md, site_title, theme)
                write_text_file(os.path.join(out_dir, target_rel), html_doc)
                exported.append({"title": title, "rel": target_rel, "excerpt": excerpt, "source": rel_path, "extra": ""})
            elif channel == "xiaohongshu":
                pages = split_xiaohongshu_pages(title, body_md)
                md_rel = change_ext(rel_path, ".xhs.md")
                html_rel = change_ext(rel_path, ".xhs.html")
                write_text_file(os.path.join(out_dir, md_rel), render_xhs_text(title, pages))
                write_text_file(os.path.join(out_dir, html_rel), render_xhs_html(title, pages, site_title, theme))
                exported.append({
                    "title": title,
                    "rel": html_rel,
                    "excerpt": excerpt,
                    "source": rel_path,
                    "extra": f"共 {len(pages)} 页图文卡片",
                })

        if cfg["generate_index"]:
            index_html = render_index_page(site_title, channel, exported, theme)
            write_text_file(os.path.join(out_dir, "index.html"), index_html)

        now_str = datetime.now().strftime("%H:%M:%S")
        count = len(exported)
        stats = {
            "last_count": count,
            "last_output": out_dir,
            "last_channel": channel,
        }
        bridge.call_host("host.storage.set", {"key": "stats", "value": stats})

        entry = {
            "title": f"{CHANNEL_LABELS[channel]} ({count} 篇)",
            "time": now_str,
            "status": "已导出" if count > 0 else "已初始化空目录",
        }
        history = [entry] + history[:19]
        bridge.call_host("host.storage.set", {"key": "history", "value": history})

        if count > 0:
            status = f"导出完成：共 {count} 篇（{CHANNEL_LABELS[channel]}）"
        else:
            status = "导出完成：当前笔记本暂无笔记，已初始化输出目录"

        page = self.build_page(cfg, stats, history, status_text=status)
        return {"values": cfg, "page": page, "status": status}

    def copy_note_assets(self, note_rel_path, body_md, out_dir):
        if not self.notebook_root:
            return
        nb_real = os.path.realpath(self.notebook_root)
        note_dir = os.path.dirname(note_rel_path)
        for match in re.finditer(r'!\[[^\]]*\]\(([^)]+)\)', body_md):
            target = match.group(1).strip().split()[0].strip("<>")
            if not target or re.match(r'^(https?:|data:|mailto:)', target, re.I):
                continue
            src_candidate = os.path.realpath(os.path.join(nb_real, note_dir, target))
            if not src_candidate.startswith(nb_real + os.sep):
                continue
            if os.path.isfile(src_candidate):
                rel_asset = os.path.relpath(src_candidate, nb_real)
                dst = os.path.join(out_dir, rel_asset)
                os.makedirs(os.path.dirname(dst), exist_ok=True)
                shutil.copy2(src_candidate, dst)


def change_ext(rel_path, new_ext):
    clean = rel_path.replace("\\", "/").lstrip("/")
    root, _ = os.path.splitext(clean)
    return root + new_ext


def write_text_file(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def extract_meta_and_body(rel_path, content):
    text = content.replace("\r\n", "\n")
    title = ""
    if text.startswith("---\n"):
        end_idx = text.find("\n---\n", 4)
        if end_idx != -1:
            fm = text[4:end_idx]
            for line in fm.splitlines():
                m = re.match(r'^title\s*:\s*(.+)$', line.strip(), re.I)
                if m:
                    title = m.group(1).strip().strip("'\"")
                    break
            text = text[end_idx + 5:]
    if not title:
        for line in text.splitlines():
            m = re.match(r'^#\s+(.+)$', line.strip())
            if m:
                title = m.group(1).strip()
                break
    if not title:
        title = os.path.splitext(os.path.basename(rel_path))[0]

    plain_lines = []
    for line in text.splitlines():
        s = line.strip()
        if not s or s.startswith("#") or s.startswith("```"):
            continue
        s = re.sub(r'!\[[^\]]*\]\([^)]*\)', '', s)
        s = re.sub(r'\[([^\]]+)\]\([^)]*\)', r'\1', s)
        s = re.sub(r'[*_`>#]', '', s).strip()
        if s:
            plain_lines.append(s)
        if sum(len(x) for x in plain_lines) >= 120:
            break
    excerpt = " ".join(plain_lines)[:120]
    return title, text.strip(), excerpt


def render_inline(text, inline_styles=None):
    placeholders = {}

    def hold(rendered):
        key = f"\x00CODE{len(placeholders)}\x00"
        placeholders[key] = rendered
        return key

    def repl_code(m):
        code_html = html.escape(m.group(1))
        if inline_styles:
            return hold(f'<code style="{inline_styles["code"]}">{code_html}</code>')
        return hold(f"<code>{code_html}</code>")

    text = re.sub(r'`([^`]+)`', repl_code, text)
    text = html.escape(text)

    def repl_img(m):
        alt = m.group(1)
        src = m.group(2).strip()
        if inline_styles:
            return f'<img src="{src}" alt="{alt}" style="{inline_styles["img"]}" />'
        return f'<img src="{src}" alt="{alt}" />'

    text = re.sub(r'!\[([^\]]*)\]\(([^)]+)\)', repl_img, text)

    def repl_link(m):
        label = m.group(1)
        href = m.group(2).strip()
        if inline_styles:
            return f'<a href="{href}" style="{inline_styles["a"]}">{label}</a>'
        return f'<a href="{href}">{label}</a>'

    text = re.sub(r'\[([^\]]+)\]\(([^)]+)\)', repl_link, text)
    text = re.sub(r'\*\*([^*]+)\*\*', r'<strong>\1</strong>', text)
    text = re.sub(r'\*([^*]+)\*', r'<em>\1</em>', text)

    for key, val in placeholders.items():
        text = text.replace(key, val)
    return text


def markdown_to_html(md_text, inline_styles=None):
    lines = md_text.replace("\r\n", "\n").splitlines()
    out = []
    i = 0
    while i < len(lines):
        line = lines[i]
        stripped = line.strip()

        if not stripped:
            i += 1
            continue

        if stripped.startswith("```"):
            lang = stripped[3:].strip()
            code_lines = []
            i += 1
            while i < len(lines) and not lines[i].strip().startswith("```"):
                code_lines.append(lines[i])
                i += 1
            i += 1
            code_body = html.escape("\n".join(code_lines))
            if inline_styles:
                out.append(f'<pre style="{inline_styles["pre"]}"><code>{code_body}</code></pre>')
            else:
                cls = f' class="language-{html.escape(lang)}"' if lang else ""
                out.append(f"<pre><code{cls}>{code_body}</code></pre>")
            continue

        m_head = re.match(r'^(#{1,6})\s+(.+)$', stripped)
        if m_head:
            level = len(m_head.group(1))
            content = render_inline(m_head.group(2).strip(), inline_styles)
            if inline_styles:
                style = inline_styles.get(f"h{level}", inline_styles["h3"])
                out.append(f'<h{level} style="{style}">{content}</h{level}>')
            else:
                out.append(f"<h{level}>{content}</h{level}>")
            i += 1
            continue

        if re.match(r'^(-{3,}|\*{3,}|_{3,})$', stripped):
            if inline_styles:
                out.append(f'<hr style="{inline_styles["hr"]}" />')
            else:
                out.append("<hr />")
            i += 1
            continue

        if stripped.startswith(">"):
            quote_lines = []
            while i < len(lines) and lines[i].strip().startswith(">"):
                quote_lines.append(lines[i].strip().lstrip(">").strip())
                i += 1
            inner = "<br />".join(render_inline(q, inline_styles) for q in quote_lines)
            if inline_styles:
                out.append(f'<blockquote style="{inline_styles["blockquote"]}">{inner}</blockquote>')
            else:
                out.append(f"<blockquote><p>{inner}</p></blockquote>")
            continue

        if re.match(r'^[-*+]\s+', stripped):
            items = []
            while i < len(lines) and re.match(r'^\s*[-*+]\s+', lines[i]):
                item_txt = re.sub(r'^\s*[-*+]\s+', '', lines[i]).strip()
                items.append(render_inline(item_txt, inline_styles))
                i += 1
            li_style = f' style="{inline_styles["li"]}"' if inline_styles else ""
            ul_style = f' style="{inline_styles["ul"]}"' if inline_styles else ""
            lis = "".join(f"<li{li_style}>{it}</li>" for it in items)
            out.append(f"<ul{ul_style}>{lis}</ul>")
            continue

        if re.match(r'^\d+\.\s+', stripped):
            items = []
            while i < len(lines) and re.match(r'^\s*\d+\.\s+', lines[i]):
                item_txt = re.sub(r'^\s*\d+\.\s+', '', lines[i]).strip()
                items.append(render_inline(item_txt, inline_styles))
                i += 1
            li_style = f' style="{inline_styles["li"]}"' if inline_styles else ""
            ol_style = f' style="{inline_styles["ol"]}"' if inline_styles else ""
            lis = "".join(f"<li{li_style}>{it}</li>" for it in items)
            out.append(f"<ol{ol_style}>{lis}</ol>")
            continue

        if "|" in stripped and i + 1 < len(lines) and re.match(r'^\|?[\s:-]+\|[\s|:-]*$', lines[i + 1].strip()):
            headers = [c.strip() for c in stripped.strip("|").split("|")]
            i += 2
            rows = []
            while i < len(lines) and "|" in lines[i].strip():
                row_cells = [c.strip() for c in lines[i].strip().strip("|").split("|")]
                rows.append(row_cells)
                i += 1
            th_s = f' style="{inline_styles["th"]}"' if inline_styles else ""
            td_s = f' style="{inline_styles["td"]}"' if inline_styles else ""
            tbl_s = f' style="{inline_styles["table"]}"' if inline_styles else ""
            thead = "<tr>" + "".join(f"<th{th_s}>{render_inline(h, inline_styles)}</th>" for h in headers) + "</tr>"
            tbody = "".join(
                "<tr>" + "".join(f"<td{td_s}>{render_inline(c, inline_styles)}</td>" for c in r) + "</tr>"
                for r in rows
            )
            out.append(f"<table{tbl_s}><thead>{thead}</thead><tbody>{tbody}</tbody></table>")
            continue

        para_lines = []
        while i < len(lines):
            cur = lines[i].strip()
            if not cur or cur.startswith("#") or cur.startswith("```") or cur.startswith(">") or re.match(r'^[-*+]\s+', cur) or re.match(r'^\d+\.\s+', cur) or re.match(r'^(-{3,}|\*{3,}|_{3,})$', cur):
                break
            para_lines.append(cur)
            i += 1
        p_html = " ".join(render_inline(p, inline_styles) for p in para_lines)
        if inline_styles:
            out.append(f'<p style="{inline_styles["p"]}">{p_html}</p>')
        else:
            out.append(f"<p>{p_html}</p>")

    return "\n".join(out)


def render_static_article(title, body_md, site_title, theme, rel_path):
    depth = rel_path.replace("\\", "/").count("/")
    home_href = ("../" * depth) + "index.html"
    article_html = markdown_to_html(body_md)
    return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>{html.escape(title)} - {html.escape(site_title)}</title>
<style>
  :root {{
    --bg: {theme["bg"]};
    --surface: {theme["surface"]};
    --text: {theme["text"]};
    --muted: {theme["muted"]};
    --accent: {theme["accent"]};
    --accent-soft: {theme["accent_soft"]};
    --border: {theme["border"]};
    --code-bg: {theme["code_bg"]};
  }}
  body {{
    margin: 0;
    background: var(--bg);
    color: var(--text);
    font-family: {theme["font"]};
    line-height: 1.75;
    padding: 32px 16px 64px;
  }}
  .wrap {{
    max-width: 740px;
    margin: 0 auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 36px 40px;
  }}
  .topbar {{
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 14px;
    color: var(--muted);
    margin-bottom: 24px;
    padding-bottom: 14px;
    border-bottom: 1px solid var(--border);
  }}
  .topbar a {{ color: var(--accent); text-decoration: none; }}
  h1, h2, h3, h4 {{ line-height: 1.35; margin-top: 1.6em; margin-bottom: 0.6em; }}
  h1 {{ font-size: 28px; margin-top: 0; }}
  h2 {{ font-size: 21px; border-bottom: 1px solid var(--border); padding-bottom: 6px; }}
  p {{ margin: 0 0 1em; }}
  a {{ color: var(--accent); }}
  code {{ background: var(--code-bg); padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }}
  pre {{ background: var(--code-bg); padding: 14px 16px; border-radius: 8px; overflow-x: auto; }}
  pre code {{ background: none; padding: 0; }}
  blockquote {{
    margin: 1em 0;
    padding: 10px 16px;
    background: var(--accent-soft);
    border-left: 3px solid var(--accent);
    color: var(--text);
    border-radius: 4px;
  }}
  img {{ max-width: 100%; border-radius: 8px; display: block; margin: 16px 0; }}
  table {{ width: 100%; border-collapse: collapse; margin: 16px 0; }}
  th, td {{ border: 1px solid var(--border); padding: 8px 12px; text-align: left; }}
  hr {{ border: none; border-top: 1px solid var(--border); margin: 24px 0; }}
</style>
</head>
<body>
  <main class="wrap">
    <div class="topbar">
      <span>{html.escape(site_title)}</span>
      <a href="{html.escape(home_href)}">返回目录</a>
    </div>
    <article>{article_html}</article>
  </main>
</body>
</html>
"""


def render_wechat_article(title, body_md, site_title, theme):
    inline_styles = {
        "h1": f"font-size:22px;font-weight:700;color:{theme['text']};margin:24px 0 14px;line-height:1.4;",
        "h2": f"font-size:18px;font-weight:700;color:{theme['accent']};margin:22px 0 12px;padding-left:10px;border-left:3px solid {theme['accent']};line-height:1.4;",
        "h3": f"font-size:16px;font-weight:700;color:{theme['text']};margin:18px 0 10px;line-height:1.4;",
        "p": f"font-size:15px;color:{theme['text']};line-height:1.8;margin:0 0 14px;letter-spacing:0.3px;",
        "blockquote": f"margin:14px 0;padding:12px 14px;background:{theme['accent_soft']};border-left:3px solid {theme['accent']};color:{theme['muted']};font-size:14px;line-height:1.7;border-radius:4px;",
        "code": f"background:{theme['code_bg']};color:{theme['accent']};padding:2px 5px;border-radius:3px;font-size:13px;font-family:Menlo,Consolas,monospace;",
        "pre": f"background:{theme['code_bg']};color:{theme['text']};padding:12px 14px;border-radius:6px;overflow-x:auto;font-size:13px;line-height:1.6;margin:14px 0;",
        "ul": f"margin:10px 0 14px;padding-left:22px;color:{theme['text']};font-size:15px;line-height:1.8;",
        "ol": f"margin:10px 0 14px;padding-left:22px;color:{theme['text']};font-size:15px;line-height:1.8;",
        "li": "margin:4px 0;",
        "a": f"color:{theme['accent']};text-decoration:underline;",
        "img": "max-width:100%;height:auto;border-radius:6px;display:block;margin:14px auto;",
        "hr": f"border:none;border-top:1px solid {theme['border']};margin:20px 0;",
        "table": f"width:100%;border-collapse:collapse;margin:14px 0;font-size:14px;",
        "th": f"border:1px solid {theme['border']};background:{theme['accent_soft']};padding:8px 10px;text-align:left;color:{theme['text']};",
        "td": f"border:1px solid {theme['border']};padding:8px 10px;text-align:left;color:{theme['text']};",
    }
    body_html = markdown_to_html(body_md, inline_styles=inline_styles)
    return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>{html.escape(title)} - 公众号排版</title>
</head>
<body style="margin:0;padding:24px 12px;background:{theme['bg']};">
  <section style="max-width:660px;margin:0 auto;background:{theme['surface']};border:1px solid {theme['border']};border-radius:8px;padding:28px 24px;font-family:{theme['font']};">
    <p style="font-size:12px;color:{theme['muted']};margin:0 0 8px;">{html.escape(site_title)}</p>
    <h1 style="font-size:24px;font-weight:700;color:{theme['text']};margin:0 0 20px;line-height:1.35;">{html.escape(title)}</h1>
    {body_html}
  </section>
</body>
</html>
"""


def split_xiaohongshu_pages(title, body_md):
    text = body_md.replace("\r\n", "\n").strip()
    raw_sections = re.split(r'\n(?:---+|(?=#{2}\s+))', text)
    pages = []
    for sec in raw_sections:
        sec = sec.strip()
        if not sec:
            continue
        lines = sec.splitlines()
        heading = title
        content_lines = []
        for idx, line in enumerate(lines):
            m = re.match(r'^#{1,3}\s+(.+)$', line.strip())
            if idx == 0 and m:
                heading = m.group(1).strip()
            else:
                content_lines.append(line)
        body_part = "\n".join(content_lines).strip()
        if not body_part:
            continue
        if len(body_part) <= 320:
            pages.append({"heading": heading, "body": body_part})
        else:
            chunks = []
            buf = []
            buf_len = 0
            for para in re.split(r'\n\s*\n', body_part):
                p = para.strip()
                if not p:
                    continue
                if buf and buf_len + len(p) > 300:
                    chunks.append("\n\n".join(buf))
                    buf = [p]
                    buf_len = len(p)
                else:
                    buf.append(p)
                    buf_len += len(p)
            if buf:
                chunks.append("\n\n".join(buf))
            for ci, chunk in enumerate(chunks):
                suffix = f" ({ci + 1}/{len(chunks)})" if len(chunks) > 1 else ""
                pages.append({"heading": heading + suffix, "body": chunk})

    if not pages:
        pages.append({"heading": title, "body": text or title})
    return pages


def render_xhs_text(title, pages):
    parts = [f"# {title}", ""]
    total = len(pages)
    for idx, p in enumerate(pages, 1):
        parts.append(f"=== 第 {idx}/{total} 页：{p['heading']} ===")
        parts.append(p["body"])
        parts.append("")
    return "\n".join(parts)


def render_xhs_html(title, pages, site_title, theme):
    cards_html = []
    total = len(pages)
    for idx, p in enumerate(pages, 1):
        body_html = markdown_to_html(p["body"])
        cards_html.append(f"""
        <div class="xhs-card">
          <div class="card-head">
            <span>{html.escape(site_title)}</span>
            <span>{idx} / {total}</span>
          </div>
          <h2>{html.escape(p["heading"])}</h2>
          <div class="card-body">{body_html}</div>
        </div>""")
    return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>{html.escape(title)} - 小红书图文分段稿</title>
<style>
  body {{
    margin: 0;
    padding: 32px 16px;
    background: {theme["bg"]};
    color: {theme["text"]};
    font-family: {theme["font"]};
  }}
  h1.doc-title {{
    text-align: center;
    font-size: 20px;
    margin: 0 0 24px;
  }}
  .grid {{
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 20px;
    max-width: 1080px;
    margin: 0 auto;
  }}
  .xhs-card {{
    background: {theme["surface"]};
    border: 1px solid {theme["border"]};
    border-radius: 14px;
    padding: 22px;
    min-height: 380px;
    display: flex;
    flex-direction: column;
  }}
  .card-head {{
    display: flex;
    justify-content: space-between;
    font-size: 12px;
    color: {theme["muted"]};
    margin-bottom: 12px;
  }}
  .xhs-card h2 {{
    font-size: 18px;
    margin: 0 0 12px;
    color: {theme["accent"]};
  }}
  .card-body {{
    font-size: 14px;
    line-height: 1.7;
  }}
  .card-body p {{ margin: 0 0 10px; }}
  .card-body code {{ background: {theme["code_bg"]}; padding: 1px 4px; border-radius: 3px; }}
</style>
</head>
<body>
  <h1 class="doc-title">{html.escape(title)}</h1>
  <div class="grid">{"".join(cards_html)}</div>
</body>
</html>
"""


def render_index_page(site_title, channel, exported, theme):
    items_html = []
    for item in exported:
        extra = f'<span class="tag">{html.escape(item["extra"])}</span>' if item["extra"] else ""
        excerpt_html = f'<p>{html.escape(item["excerpt"])}</p>' if item["excerpt"] else ""
        items_html.append(
            f'<li class="entry"><a href="{html.escape(item["rel"])}">{html.escape(item["title"])}</a>{extra}{excerpt_html}</li>'
        )
    list_block = f'<ul class="list">{"".join(items_html)}</ul>' if items_html else '<p class="empty">当前笔记本暂无可导出笔记</p>'
    return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>{html.escape(site_title)}</title>
<style>
  body {{
    margin: 0;
    padding: 40px 16px;
    background: {theme["bg"]};
    color: {theme["text"]};
    font-family: {theme["font"]};
  }}
  .wrap {{
    max-width: 720px;
    margin: 0 auto;
    background: {theme["surface"]};
    border: 1px solid {theme["border"]};
    border-radius: 12px;
    padding: 32px 36px;
  }}
  h1 {{ margin: 0 0 6px; font-size: 26px; }}
  .meta {{ font-size: 13px; color: {theme["muted"]}; margin-bottom: 24px; }}
  .list {{ list-style: none; padding: 0; margin: 0; }}
  .entry {{ padding: 14px 0; border-top: 1px solid {theme["border"]}; }}
  .entry a {{ font-size: 17px; font-weight: 600; color: {theme["accent"]}; text-decoration: none; }}
  .entry p {{ margin: 6px 0 0; font-size: 14px; color: {theme["muted"]}; line-height: 1.6; }}
  .tag {{ margin-left: 8px; font-size: 12px; background: {theme["accent_soft"]}; color: {theme["accent"]}; padding: 2px 8px; border-radius: 999px; }}
  .empty {{ color: {theme["muted"]}; }}
</style>
</head>
<body>
  <main class="wrap">
    <h1>{html.escape(site_title)}</h1>
    <div class="meta">渠道：{html.escape(CHANNEL_LABELS[channel])} · 共 {len(exported)} 篇</div>
    {list_block}
  </main>
</body>
</html>
"""


def main():
    bridge = HostBridge()
    plugin = PublisherPlugin()
    bridge.serve(plugin.handle)


if __name__ == "__main__":
    main()
