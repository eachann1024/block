"""Block Tag Manager. The host owns the UI, note I/O and file revisions."""
import hashlib
import json
import sys
import time
from collections import defaultdict, deque
from pathlib import PurePosixPath

from frontmatter import Properties, Unsupported, valid_tag, yaml


class HostError(RuntimeError):
    pass


class Host:
    def __init__(self):
        self.seq = 0

    def call(self, method, **params):
        self.seq += 1
        key = "tags-" + str(self.seq)
        send({"id": key, "method": method, "params": params})
        reply = json.loads(sys.stdin.readline())
        if reply.get("id") != key:
            raise HostError("Unexpected host response")
        if "error" in reply:
            raise HostError(reply["error"]["message"])
        return reply["result"]


def send(message):
    print(json.dumps({"jsonrpc": "2.0", **message}, ensure_ascii=False), flush=True)


def token(*parts):
    return hashlib.sha256(json.dumps(parts, ensure_ascii=False).encode()).hexdigest()[:32]


def score(tag, query):
    name = tag.casefold()
    q = query.casefold().strip().lstrip("#")
    if not q:
        return 1
    leaf = name.rsplit("/", 1)[-1]
    if leaf == q:
        return 5
    if leaf.startswith(q):
        return 4
    if q in name:
        return 3
    chars = iter(name)
    return 2 if all(c in chars for c in q) else 0


class Notebook:
    def __init__(self, host):
        self.host = host
        self.files = {}
        self.tags = {}
        self.pending = deque()
        self.unreadable = set()
        self.checked = None

    def update(self):
        now = time.monotonic()
        if self.checked is None or (not self.pending and now - self.checked >= 5):
            files = {n["path"]: (n["mod_time"], n["size"]) for n in self.host.call("host.notes.list")["notes"]}
            self.pending = deque(p for p in files if p not in self.tags or files[p] != self.files.get(p))
            self.tags = {p: tags for p, tags in self.tags.items() if p in files}
            self.unreadable.intersection_update(files)
            self.files, self.checked = files, now
        # Share the host's render budget with typing. Continue on the next visible refresh.
        deadline = time.monotonic() + 0.32
        while self.pending and time.monotonic() < deadline:
            path = self.pending.popleft()
            try:
                self.tags[path] = Properties(self.host.call("host.notes.read", path=path)["content"]).tags
                self.unreadable.discard(path)
            except (HostError, Unsupported, yaml.YAMLError, RecursionError):
                self.unreadable.add(path)
                self.tags[path] = []

    def directory(self):
        names, notes, explicit = {}, defaultdict(set), set()
        for path, tags in self.tags.items():
            for tag in tags:
                if not all(tag.split("/")):
                    continue
                parts = tag.split("/")
                for i in range(1, len(parts) + 1):
                    name = "/".join(parts[:i])
                    key = name.casefold()
                    names.setdefault(key, name)
                    notes[key].add(path)
                explicit.add(tag.casefold())
        return names, notes, explicit


class Manager:
    def __init__(self, host):
        self.host = host
        self.notebook = Notebook(host)
        self.english = False
        self.path = self.draft = self.editing = self.query = self.selected = ""
        self.note_page = self.tag_page = self.suggestion_page = 0
        self.actions = {}
        self.collapsed = set()

    def tr(self, zh, en):
        return en if self.english else zh

    def action_id(self, kind, *args):
        key = kind + "-" + token(*args)
        self.actions[key] = (kind, args)
        return key

    def row(self, title, status, kind, *args):
        return {"title": title, "status": status, "action_id": self.action_id(kind, *args)}

    def log(self, rows, empty=None, key=""):
        return {"kind": "log_list", "id": key, "entries": rows, "empty_text": empty or self.tr("没有匹配项", "No matches")}

    def buttons(self, *items):
        return {"kind": "actions", "buttons": [
            {"id": self.action_id(kind, *args), "label": label, "variant": variant}
            for label, variant, kind, args in items]}

    def input(self, key, label, value):
        return {"kind": "input", "id": key, "label": label, "value": value, "layout": "stacked"}

    def paged(self, rows, attr, size):
        page = max(0, min(getattr(self, attr), (len(rows) - 1) // size))
        setattr(self, attr, page)
        items = [self.log(rows[page * size:(page + 1) * size], key=attr)]
        nav = []
        if page:
            nav.append((self.tr("上一页", "Previous"), "secondary", "page", (attr, page - 1)))
        if len(rows) > (page + 1) * size:
            nav.append((self.tr("下一页", "Next"), "secondary", "page", (attr, page + 1)))
        if nav:
            items.append(self.buttons(*nav))
        return items

    def render(self, changes=None):
        tr = self.tr
        self.notebook.update()
        path = self.host.call("host.notes.active").get("path", "")
        if path != self.path:
            self.path, self.editing, self.draft = path, "", ""
        self.actions = {}
        draft_id = "tag-" + token(path)
        changes = changes or {}
        if draft_id in changes and changes[draft_id] != self.draft:
            self.draft, self.suggestion_page = str(changes[draft_id]), 0
        if "filter" in changes and changes["filter"] != self.query:
            self.query, self.tag_page, self.note_page, self.selected = str(changes["filter"]), 0, 0, ""
        names, notes, explicit = self.notebook.directory()
        cards = []
        if path in self.notebook.files:
            try:
                note = self.host.call("host.notes.read", path=path)
                props = Properties(note["content"])
                if props.fields:
                    fields = [{"title": k, "sub_text": v[:500]} for k, v in props.fields if k.strip()]
                    if fields:
                        cards.append(dict(id="properties", title=tr("笔记属性", "Properties"), collapsible=True,
                                          default_collapsed=True, items=[self.log(fields, key="fields")]))
                tag_items = []
                if props.tags:
                    tag_items.append(self.log([self.row(t, tr("编辑", "Edit"), "edit", path, t) for t in props.tags], key="assigned"))
                if self.editing and self.editing not in props.tags:
                    self.editing, self.draft = "", ""
                tag_items.append(self.input(draft_id, tr("重命名标签", "Rename tag") if self.editing else tr("添加标签", "Add tag"), self.draft))
                name = self.draft.strip().lstrip("#")
                target = path, note["revision"], self.editing
                assigned = {t.casefold() for t in props.tags}
                if name:
                    matches = [(score(names[k], name), k) for k in explicit if k not in assigned]
                    matches = sorted(((s, k) for s, k in matches if s), key=lambda e: (-e[0], -len(notes[e[1]]), e[1]))
                    if matches:
                        suggestions = [self.row(names[k], tr("选用", "Choose"), "save", *target, names[k]) for _, k in matches]
                        tag_items.extend(self.paged(suggestions, "suggestion_page", 8))
                buttons = []
                if name and valid_tag(name) and name != self.editing and (name.casefold() not in assigned or name.casefold() == self.editing.casefold()):
                    buttons.append((tr("保存标签", "Save tag"), "primary", "save", (*target, name)))
                if self.editing:
                    buttons.extend([(tr("移除标签", "Remove tag"), "danger", "remove", target),
                                    (tr("取消", "Cancel"), "secondary", "cancel", ())])
                if buttons:
                    tag_items.append(self.buttons(*buttons))
                cards.append(dict(id="note", title=tr("当前笔记", "Current note"), meta=PurePosixPath(path).stem, items=tag_items))
            except (HostError, Unsupported, yaml.YAMLError, RecursionError):
                cards.append(dict(id="note", title=tr("当前笔记", "Current note"), items=[
                    self.log([], tr("无法编辑这篇笔记的 YAML，请检查源码。", "Cannot edit this note’s YAML. Check its source."))]))
        browse = [self.input("filter", tr("筛选标签", "Filter tags"), self.query)]
        query = (self.query[4:] if self.query.lower().startswith("tag:") else self.query).strip().lstrip("#")
        if self.selected and self.selected not in names and not self.notebook.pending:
            self.selected = ""
        if self.selected:
            browse.append(self.buttons((tr("全部标签", "All tags"), "secondary", "browse", ("",))))
        else:
            children = defaultdict(list)
            for k in names:
                children[k.rpartition("/")[0]].append(k)
            def order(k):
                return -len(notes[k]), k
            rows = []
            # Use an explicit stack so deeply nested frontmatter tags are safe.
            stack = [(k, 0) for k in sorted(children[""], key=order, reverse=True) if k]
            if query:
                for k in sorted(names, key=lambda k: (-score(names[k], query), *order(k))):
                    if score(names[k], query):
                        rows.append(self.row(names[k], str(len(notes[k])), "browse", k))
            else:
                while stack:
                    k, depth = stack.pop()
                    prefix = "　" * min(depth, 6)
                    leaf = names[k].rsplit("/", 1)[-1] or names[k]
                    if children[k]:
                        marker = "▸ " if k in self.collapsed else "▾ "
                        rows.append(self.row(prefix + marker + leaf, str(len(notes[k])), "fold", k))
                        if k in self.collapsed:
                            continue
                        rows.append(self.row(prefix + "　" + tr("全部笔记", "All notes"), str(len(notes[k])), "browse", k))
                    else:
                        rows.append(self.row(prefix + leaf, str(len(notes[k])), "browse", k))
                    stack.extend((child, depth + 1) for child in sorted(children[k], key=order, reverse=True))
            browse.extend(self.paged(rows, "tag_page", 80))
        cards.append(dict(id="directory", title=tr("笔记本标签", "Notebook tags"), items=browse))
        if self.selected:
            paths = sorted(notes[self.selected], key=str.casefold)
            rows = [self.row(PurePosixPath(p).stem, "", "open", p) for p in paths]
            for row, p in zip(rows, paths):
                row["tooltip"] = p
            cards.append(dict(id="matches", title=tr("匹配的笔记", "Matching notes"), meta=f"{names.get(self.selected, self.selected)} ({len(paths)})",
                              items=self.paged(rows, "note_page", 40)))
        status = ""
        if self.notebook.pending:
            status = tr("正在读取标签", "Reading tags") + f" ({len(self.notebook.files)-len(self.notebook.pending)}/{len(self.notebook.files)})"
        elif self.notebook.unreadable:
            status = tr("未能读取标签的笔记", "Notes with unreadable tags") + f" ({len(self.notebook.unreadable)})"
        return {"page": dict(title=tr("标签管理", "Tag Manager"), cards=cards, status=status,
                             refresh_interval_ms=500 if self.notebook.pending else 5000),
                "values": {draft_id: self.draft, "filter": self.query}}

    def action(self, params):
        current = self.actions.get(params["action_id"])
        if current is None:
            return dict(ok=False, status=self.tr("列表已更新，请重新选择。", "The list changed. Select again."), **self.render())
        kind, args = current
        if kind == "page":
            setattr(self, args[0], args[1])
        elif kind == "fold":
            self.collapsed.symmetric_difference_update([args[0]])
        elif kind == "browse":
            self.selected, self.tag_page, self.note_page = args[0], 0, 0
        elif kind == "open":
            self.host.call("host.notes.open", path=args[0])
        elif kind == "cancel":
            self.editing, self.draft = "", ""
        elif kind in ("edit", "save", "remove"):
            path = args[0]
            if self.host.call("host.notes.active").get("path") != path:
                return dict(ok=False, status=self.tr("当前笔记已切换，请重新选择。", "The active note changed. Select again."), **self.render())
            if kind == "edit":
                self.editing = self.draft = args[1]
            else:
                before = self.host.call("host.notes.read", path=path)
                if before["revision"] != args[1]:
                    return dict(ok=False, status=self.tr("笔记已更新，请重新选择。", "The note changed. Select again."), **self.render())
                props = Properties(before["content"])
                tags = list(props.tags)
                old = args[2]
                if kind == "remove":
                    tags.remove(old)
                elif old:
                    tags[tags.index(old)] = args[3]
                else:
                    tags.append(args[3])
                if kind == "save" and (not valid_tag(args[3]) or len({t.casefold() for t in tags}) != len(tags)):
                    return dict(ok=False, status=self.tr("标签无效或已存在。", "Invalid or duplicate tag."))
                try:
                    content = props.replace(tags)
                    self.host.call("host.notes.write", path=path, content=content, expected_revision=before["revision"])
                except (Unsupported, yaml.YAMLError):
                    return dict(ok=False, status=self.tr("请在 YAML 源码中修改这个标签。", "Edit this tag in the YAML source."))
                except HostError as err:
                    return dict(ok=False, status=self.tr("保存失败。", "Could not save. ") + str(err))
                self.notebook.tags[path] = tags
                self.editing, self.draft = "", ""
        return dict(ok=True, **self.render())

    def handle(self, method, params):
        if method == "plugin.initialize":
            self.english = params.get("locale") == "en"
            return {"ready": True}
        if method == "plugin.command" and params.get("command_id") == "show":
            return self.host.call("host.ui.open_panel", id="tags")
        if method == "plugin.render" and params.get("surface") == "settings_page":
            tr = self.tr
            return {"page": {"title": tr("标签管理", "Tag Manager"), "cards": [{
                "id": "panel", "title": tr("使用标签面板", "Using the tags panel"), "items": [{
                    "kind": "kv", "rows": [
                        {"label": tr("打开面板", "Open panel"), "value": tr("笔记右上角 → … → 显示标签面板", "Note toolbar → … → Show Tags Panel")},
                        {"label": tr("添加标签", "Add tags"), "value": tr("输入标签名，或输入一个字选择已有标签。", "Enter a new name or type a character to choose an existing tag.")},
                    ],
                }],
            }]}}
        if method == "plugin.render":
            return self.render(params.get("values"))
        if method == "plugin.action":
            return self.action(params)
        if method == "plugin.event":
            if params.get("event") == "note.saved":
                self.notebook.tags.pop(params.get("path"), None)
                self.notebook.checked = None
            return {"handled": True}
        if method == "plugin.shutdown":
            return {"ok": True}
        raise ValueError("Unknown request: " + method)


if __name__ == "__main__":
    manager = Manager(Host())
    for line in sys.stdin:
        req = json.loads(line)
        try:
            result = manager.handle(req["method"], req.get("params") or {})
            send({"id": req["id"], "result": result})
        except Exception as err:
            send({"id": req["id"], "error": {"code": -32000, "message": str(err)}})
        if req.get("method") == "plugin.shutdown":
            break
