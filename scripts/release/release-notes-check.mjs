import assert from "node:assert/strict";

export const EN_TITLE = "## English";
export const ZH_TITLE = "## 中文";
export const EN_SECTIONS = ["### New", "### Improvements", "### Fixes"];
export const ZH_SECTIONS = ["### 新功能", "### 改进", "### 修复"];
export const WEBSITE = "https://block.cooool.top";
export const MAX_PER_SECTION = 8;

const CJK = /[\u3400-\u9fff]/;
const EN_CLAUSE = /[A-Za-z]{2,}(?:['’][A-Za-z]+)?(?:\s+[A-Za-z]{2,}(?:['’][A-Za-z]+)?){1,}/;
const BANNED = /`|#\d+|\b[0-9a-f]{7,40}\b|\b(?:gpui|wgpu|wasm|ipc|debounce|typecheck)\b/i;
const DATE_LINE = /^(?:📅|\d{4}-\d{2}-\d{2}\b|##\s+\d{4})/;

export const EN_FOOTER = [
  "### Download",
  "",
  "Download the macOS, Windows, or Linux installer from the files attached to this release.",
  "",
  `Website: ${WEBSITE}`,
].join("\n");

export const ZH_FOOTER = [
  "### 下载",
  "",
  "macOS、Windows 和 Linux 的安装包可以在本页附件里下载。",
  "",
  `官网：${WEBSITE}`,
].join("\n");

export function lineMixesLanguages(line) {
  const plain = String(line).replace(/https?:\/\/\S+/g, " ");
  const cjk = CJK.test(plain);
  const clause = EN_CLAUSE.test(plain);
  if (cjk && clause) return true;
  if (!cjk && /[A-Za-z]/.test(plain) && /[。！？]/.test(plain)) return true;
  if (cjk && /[.!?]\s*$/.test(plain.trim())) return true;
  return false;
}

function stripFence(text) {
  const trimmed = String(text ?? "").replace(/\r\n?/g, "\n").trim();
  return trimmed.replace(/^```(?:markdown|md)?\n([\s\S]*?)\n```$/i, "$1").trim();
}

function changelogLines(half, footerHeading) {
  const lines = half.split("\n").map((line) => line.trim()).filter(Boolean);
  const cut = lines.findIndex((line) => line === footerHeading || line.startsWith("Website:") || line.startsWith("官网："));
  return cut >= 0 ? lines.slice(0, cut) : lines;
}

function rebuildHalf(half, { title, sections, footerHeading, footer, cjk }) {
  const lines = changelogLines(half, footerHeading);
  assert.equal(lines[0], title, `expected ${title}`);
  const out = [title, ""];
  const present = [];
  const counts = [];
  let index = -1;
  let count = 0;
  const end = cjk ? /[。！？]$/ : /[.!?]$/;
  const punct = cjk ? "。" : ".";
  for (const line of lines.slice(1)) {
    assert.ok(!DATE_LINE.test(line), `dated section is not allowed: ${line}`);
    const section = sections.indexOf(line);
    if (section >= 0) {
      assert.ok(section > index, `section order: ${line}`);
      assert.ok(index < 0 || count > 0, `empty section: ${sections[index]}`);
      if (present.length) out.push("");
      out.push(line);
      present.push(line);
      if (index >= 0) counts.push(count);
      index = section;
      count = 0;
      continue;
    }
    const bullet = /^[-*•]\s+(.+)$/.exec(line);
    assert.ok(bullet && index >= 0, `invalid line: ${line}`);
    let item = bullet[1].replace(/\*\*/g, "").trim();
    assert.ok(!BANNED.test(item), `technical wording: ${item}`);
    if (!end.test(item)) item += punct;
    const rendered = `- ${item}`;
    assert.ok(!lineMixesLanguages(rendered), `mixed language line: ${rendered}`);
    if (cjk) assert.ok(CJK.test(item), `Chinese bullet has no Chinese: ${item}`);
    else assert.ok(!CJK.test(item), `English bullet has Chinese: ${item}`);
    count += 1;
    assert.ok(count <= MAX_PER_SECTION, `more than ${MAX_PER_SECTION} bullets`);
    out.push(rendered);
  }
  assert.ok(index >= 0 && count > 0, `${title} has no changes`);
  counts.push(count);
  out.push("", footer);
  return { text: out.join("\n"), present, counts };
}

export function validateReleaseNotes(raw) {
  const text = stripFence(raw);
  if (!text || /^[（(]?(?:无|none)[)）]?[.。]?$/i.test(text)) return "";
  const parts = text.split(/\n\s*---\s*\n/);
  assert.equal(parts.length, 2, "English, a --- line, then Chinese");
  assert.ok(!DATE_LINE.test(text), "dated sections are not allowed");
  const en = rebuildHalf(parts[0], {
    title: EN_TITLE,
    sections: EN_SECTIONS,
    footerHeading: "### Download",
    footer: EN_FOOTER,
    cjk: false,
  });
  const zh = rebuildHalf(parts[1], {
    title: ZH_TITLE,
    sections: ZH_SECTIONS,
    footerHeading: "### 下载",
    footer: ZH_FOOTER,
    cjk: true,
  });
  assert.deepEqual(en.present.map((h) => EN_SECTIONS.indexOf(h)), zh.present.map((h) => ZH_SECTIONS.indexOf(h)), "English and Chinese sections differ");
  assert.deepEqual(en.counts, zh.counts, "English and Chinese bullet counts differ");
  const body = `${en.text}\n\n---\n\n${zh.text}`;
  for (const line of body.split("\n")) {
    if (line.trim()) assert.ok(!lineMixesLanguages(line), `mixed language line: ${line}`);
  }
  assert.ok(body.includes(`Website: ${WEBSITE}`) && body.includes(`官网：${WEBSITE}`), "missing website");
  assert.ok(body.includes("### Download") && body.includes("### 下载"), "missing download section");
  return body;
}
