import unittest

from frontmatter import Properties, Unsupported
from main import Manager


class FrontmatterTest(unittest.TestCase):
    def test_preserve_neighbors_and_body(self):
        prefix = "\ufeff--- \r\ntitle: '示例' # keep\r\ntags:\r\n"
        suffix = "\r\n# keep\r\nother: [a, b]\r\n---\r\n\r\nbody without final newline"
        out = Properties(prefix + "- one\r\n- two" + suffix).replace(["工作/账号"])
        self.assertTrue(out.startswith(prefix))
        self.assertTrue(out.endswith(suffix))
        self.assertEqual(Properties(out).tags, ["工作/账号"])

    def test_yaml_forms(self):
        for value in ['tag', '["tag"]', '\n- tag', '\n  - tag']:
            props = Properties(f"---\ntags: {value}\n---\nbody")
            self.assertEqual(props.tags, ["tag"])
            self.assertEqual(Properties(props.replace([])).tags, [])
        self.assertEqual(Properties("\ufeffbody").replace(["标签"]), '\ufeff---\ntags: ["标签"]\n---\nbody')

    def test_do_not_destroy_comments_or_aliases(self):
        for src in ('tags: [one, # comment\n two]', 'x: &one [one]\ntags: *one'):
            with self.assertRaises(Unsupported):
                Properties("---\n" + src + "\n---").replace(["新"])


class FakeHost:
    def __init__(self):
        self.active = "one.md"
        self.notes = {"one.md": "---\ntags: [工作/需求]\n---\nBody", "two.md": "---\ntags: [工作/账号, 生活/账号]\n---"}

    def call(self, method, **params):
        if method == "host.notes.active":
            return {"path": self.active}
        if method == "host.notes.list":
            return {"notes": [dict(path=p, size=len(t), mod_time="today") for p, t in self.notes.items()]}
        if method == "host.notes.read":
            text = self.notes[params["path"]]
            return dict(content=text, revision=str(hash(text)))
        if method == "host.notes.write":
            assert params["expected_revision"] == str(hash(self.notes[params["path"]]))
            self.notes[params["path"]] = params["content"]
            return {"ok": True}


class PanelTest(unittest.TestCase):
    def test_suggestions_and_stale_actions(self):
        host = FakeHost()
        manager = Manager(host)
        form = manager.render()["values"]
        field = next(k for k in form if k.startswith("tag-"))
        manager.render({field: "账"})

        def suggested():
            return {args[-1]: key for key, (kind, args) in manager.actions.items()
                    if kind == "save" and args[-1].endswith("账号")}

        self.assertEqual(set(suggested()), {"工作/账号", "生活/账号"})
        old = suggested()["生活/账号"]
        host.active = "two.md"
        self.assertFalse(manager.action({"action_id": old})["ok"])
        host.active = "one.md"
        manager.render({field: "账"})
        old = suggested()["工作/账号"]
        host.notes["one.md"] += " changed externally"
        self.assertFalse(manager.action({"action_id": old})["ok"])
        self.assertTrue(manager.action({"action_id": suggested()["工作/账号"]})["ok"])
        self.assertEqual(Properties(host.notes["one.md"]).tags, ["工作/需求", "工作/账号"])
        self.assertTrue(host.notes["one.md"].endswith("Body changed externally"))
        _, docs, _ = manager.notebook.directory()
        self.assertEqual(len(docs["工作"]), 2)


if __name__ == "__main__":
    unittest.main()
