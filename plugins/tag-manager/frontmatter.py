"""Read YAML nodes without constructing objects. Only splice the tags value."""
import json
import re
import sys
import unicodedata
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "vendor"))
import yaml

OPEN = re.compile(r"\A---[ \t]*\r?\n")
CLOSE = re.compile(r"^(?:---|\.\.\.)[ \t]*\r?(?:\n|\Z)", re.M)


class Unsupported(ValueError):
    pass


class Properties:
    def __init__(self, text):
        self.bom = "\ufeff" if text.startswith("\ufeff") else ""
        self.text = text[len(self.bom):]
        self.eol = "\r\n" if self.text.partition("\n")[0].endswith("\r") else "\n"
        self.open = OPEN.match(self.text)
        self.tags = []
        self.fields = []
        self.tag_key = self.value = None
        if not self.open:
            return
        self.close = CLOSE.search(self.text, self.open.end())
        if not self.close:
            raise Unsupported("Unclosed YAML header")
        self.header = self.text[self.open.end():self.close.start()]
        root = yaml.compose(self.header, Loader=yaml.SafeLoader)
        if root is None:
            return
        if not isinstance(root, yaml.MappingNode) or root.flow_style:
            raise Unsupported("Expected a block mapping")
        for key, node in root.value:
            if not isinstance(key, yaml.ScalarNode):
                raise Unsupported("Unsupported property key")
            if key.value != "tags":
                value = node.value if isinstance(node, yaml.ScalarNode) else self.header[node.start_mark.index:node.end_mark.index].strip()
                self.fields.append((key.value, value))
                continue
            if self.tag_key is not None:
                raise Unsupported("Duplicate tags property")
            self.tag_key, self.value = key, node
            if isinstance(node, yaml.SequenceNode):
                items = node.value
            elif isinstance(node, yaml.ScalarNode):
                items = [] if node.tag.endswith(":null") else [node]
            else:
                raise Unsupported("Expected a tag or a list of tags")
            for item in items:
                if not isinstance(item, yaml.ScalarNode):
                    raise Unsupported("Unsupported tag")
                name = item.value.strip().lstrip("#")
                if name and name.casefold() not in {t.casefold() for t in self.tags}:
                    self.tags.append(name)

    def replace(self, tags):
        value = json.dumps(tags, ensure_ascii=False)
        if not self.open:
            fence = "---" + self.eol
            return self.bom + fence + "tags: " + value + self.eol + fence + self.text
        header = self.header
        if self.tag_key is None:
            header += "tags: " + value + self.eol
        else:
            # Aliases can point outside this entry. Never edit through an alias.
            tokens = list(yaml.scan(header))
            if any(isinstance(t, (yaml.AliasToken, yaml.AnchorToken)) for t in tokens):
                raise Unsupported("Edit anchored YAML in the source")
            node = self.value
            start, end = node.start_mark.index, node.end_mark.index
            if isinstance(node, yaml.SequenceNode) and not node.flow_style:
                # A block sequence ends at the *next key* in the YAML syntax tree.
                # Stop at the last scalar instead, preserving following fields.
                end = node.value[-1].end_mark.index
                if not node.start_mark.column:
                    value = "  " + value
            if start == end and not header[:start].endswith((" ", "\t")):
                value = " " + value
            cursor = start
            for t in tokens:
                if t.start_mark.index < start or t.end_mark.index > end:
                    continue
                if "#" in header[cursor:t.start_mark.index]:
                    raise Unsupported("Edit commented tag lists in the source")
                cursor = max(cursor, t.end_mark.index)
            suffix = header[end:]
            if header[start:end].endswith("\n") and not suffix.startswith(("\r", "\n")):
                value += self.eol
            header = header[:start] + value + suffix
        updated = self.bom + self.text[:self.open.end()] + header + self.text[self.close.start():]
        # Confirm that YAML indentation or an unusual scalar style did not change
        # the meaning of another property before requesting a write.
        check = Properties(updated)
        if check.tags != tags or check.fields != self.fields:
            raise Unsupported("Cannot safely edit this YAML value")
        return updated


def valid_tag(name):
    return (bool(name) and all(name.split("/")) and not name.isnumeric()
            and all(c in "/-_" or unicodedata.category(c)[0] in "LMN" for c in name))
