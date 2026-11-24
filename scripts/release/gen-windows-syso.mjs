import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const DEFAULT_ICO = resolve(ROOT, "resources/icon.ico");
const DEFAULT_SYSO = resolve(ROOT, "cmd/block/rsrc_windows_amd64.syso");

const RT_ICON = 3;
const RT_GROUP_ICON = 14;
const LANG_EN_US = 0x0409;
const IMAGE_FILE_MACHINE_AMD64 = 0x8664;
const IMAGE_SCN_CNT_INITIALIZED_DATA = 0x00000040;
const IMAGE_SCN_MEM_READ = 0x40000000;
const IMAGE_REL_AMD64_ADDR32NB = 0x0003;
const IMAGE_SYM_CLASS_STATIC = 3;

function align4(n) {
  return (n + 3) & ~3;
}

function parseIco(buf) {
  assert.ok(buf.length >= 6, "icon.ico is truncated");
  const reserved = buf.readUInt16LE(0);
  const type = buf.readUInt16LE(2);
  const count = buf.readUInt16LE(4);
  assert.equal(reserved, 0, "invalid ICO reserved header");
  assert.equal(type, 1, "icon.ico is not an ICO resource");
  assert.ok(count > 0, "icon.ico contains no images");
  assert.ok(buf.length >= 6 + count * 16, "icon.ico directory is truncated");

  const entries = [];
  for (let i = 0; i < count; i += 1) {
    const off = 6 + i * 16;
    const width = buf.readUInt8(off);
    const height = buf.readUInt8(off + 1);
    const colorCount = buf.readUInt8(off + 2);
    const reservedByte = buf.readUInt8(off + 3);
    const planes = buf.readUInt16LE(off + 4) || 1;
    const bitCount = buf.readUInt16LE(off + 6) || 32;
    const bytesInRes = buf.readUInt32LE(off + 8);
    const imageOffset = buf.readUInt32LE(off + 12);
    assert.ok(
      bytesInRes > 0 && imageOffset + bytesInRes <= buf.length,
      `icon.ico entry ${i} out of bounds`,
    );
    entries.push({
      id: i + 1,
      width,
      height,
      colorCount,
      reservedByte,
      planes,
      bitCount,
      data: buf.subarray(imageOffset, imageOffset + bytesInRes),
    });
  }
  return entries;
}

function buildGroupIconData(entries) {
  const buf = Buffer.alloc(6 + entries.length * 14);
  buf.writeUInt16LE(0, 0); // reserved
  buf.writeUInt16LE(1, 2); // type = icon
  buf.writeUInt16LE(entries.length, 4);
  entries.forEach((e, idx) => {
    const off = 6 + idx * 14;
    buf.writeUInt8(e.width, off);
    buf.writeUInt8(e.height, off + 1);
    buf.writeUInt8(e.colorCount, off + 2);
    buf.writeUInt8(e.reservedByte, off + 3);
    buf.writeUInt16LE(e.planes, off + 4);
    buf.writeUInt16LE(e.bitCount, off + 6);
    buf.writeUInt32LE(e.data.length, off + 8);
    buf.writeUInt16LE(e.id, off + 12);
  });
  return buf;
}

// Build a Windows x86_64 COFF .syso containing a .rsrc section with RT_ICON and RT_GROUP_ICON.
export function buildIconSysoBuffer(icoBuf) {
  const icons = parseIco(icoBuf);
  const groupIconBuf = buildGroupIconData(icons);

  // Resource tree: type -> id -> lang -> data
  // Sorted ascending by numeric ID at every level as required by Windows PE loader.
  const types = [
    {
      id: RT_ICON,
      items: icons.map((icon) => ({ id: icon.id, lang: LANG_EN_US, data: icon.data })),
    },
    {
      id: RT_GROUP_ICON,
      items: [{ id: 1, lang: LANG_EN_US, data: groupIconBuf }],
    },
  ];

  // Layout pass: compute exact section-relative offsets for directories, data entries, and payloads.
  const dirSize = (n) => 16 + n * 8;
  let cursor = 0;

  const rootOff = cursor;
  cursor += dirSize(types.length);

  for (const t of types) {
    t.dirOff = cursor;
    cursor += dirSize(t.items.length);
  }
  for (const t of types) {
    for (const item of t.items) {
      item.langDirOff = cursor;
      cursor += dirSize(1);
    }
  }
  const relocs = [];
  for (const t of types) {
    for (const item of t.items) {
      item.dataEntryOff = cursor;
      relocs.push(cursor); // OffsetToData is at +0 of IMAGE_RESOURCE_DATA_ENTRY
      cursor += 16;
    }
  }
  for (const t of types) {
    for (const item of t.items) {
      cursor = align4(cursor);
      item.dataOff = cursor;
      cursor += item.data.length;
    }
  }
  const rsrcSize = align4(cursor);
  const rsrc = Buffer.alloc(rsrcSize);

  const writeDir = (off, entries) => {
    rsrc.writeUInt16LE(0, off + 12); // NumberOfNamedEntries
    rsrc.writeUInt16LE(entries.length, off + 14); // NumberOfIdEntries
    entries.forEach((e, idx) => {
      const eOff = off + 16 + idx * 8;
      rsrc.writeUInt32LE(e.id >>> 0, eOff);
      rsrc.writeUInt32LE((e.target | (e.isDir ? 0x80000000 : 0)) >>> 0, eOff + 4);
    });
  };

  writeDir(
    rootOff,
    types.map((t) => ({ id: t.id, target: t.dirOff, isDir: true })),
  );
  for (const t of types) {
    writeDir(
      t.dirOff,
      t.items.map((item) => ({ id: item.id, target: item.langDirOff, isDir: true })),
    );
    for (const item of t.items) {
      writeDir(item.langDirOff, [{ id: item.lang, target: item.dataEntryOff, isDir: false }]);
      rsrc.writeUInt32LE(item.dataOff >>> 0, item.dataEntryOff);
      rsrc.writeUInt32LE(item.data.length >>> 0, item.dataEntryOff + 4);
      rsrc.writeUInt32LE(0, item.dataEntryOff + 8); // CodePage
      rsrc.writeUInt32LE(0, item.dataEntryOff + 12); // Reserved
      item.data.copy(rsrc, item.dataOff);
    }
  }

  // Assemble COFF object: FileHeader(20) + SectionHeader(40) + .rsrc + Relocs(10*N) + SymTab(18) + StrTab(4)
  const headerSize = 20;
  const secHeaderSize = 40;
  const rsrcRawPtr = headerSize + secHeaderSize;
  const relocPtr = rsrcRawPtr + rsrc.length;
  const relocSize = relocs.length * 10;
  const symTabPtr = relocPtr + relocSize;
  const symTabSize = 18;
  const strTabSize = 4;

  const out = Buffer.alloc(symTabPtr + symTabSize + strTabSize);

  // COFF File Header
  out.writeUInt16LE(IMAGE_FILE_MACHINE_AMD64, 0);
  out.writeUInt16LE(1, 2); // NumberOfSections
  out.writeUInt32LE(0, 4); // TimeDateStamp
  out.writeUInt32LE(symTabPtr >>> 0, 8);
  out.writeUInt32LE(1, 12); // NumberOfSymbols
  out.writeUInt16LE(0, 16); // SizeOfOptionalHeader
  out.writeUInt16LE(0, 18); // Characteristics

  // Section Header (.rsrc)
  out.write(".rsrc", headerSize, "ascii");
  out.writeUInt32LE(0, headerSize + 8); // VirtualSize
  out.writeUInt32LE(0, headerSize + 12); // VirtualAddress
  out.writeUInt32LE(rsrc.length >>> 0, headerSize + 16); // SizeOfRawData
  out.writeUInt32LE(rsrcRawPtr >>> 0, headerSize + 20); // PointerToRawData
  out.writeUInt32LE(relocPtr >>> 0, headerSize + 24); // PointerToRelocations
  out.writeUInt32LE(0, headerSize + 28); // PointerToLinenumbers
  out.writeUInt16LE(relocs.length, headerSize + 32); // NumberOfRelocations
  out.writeUInt16LE(0, headerSize + 34); // NumberOfLinenumbers
  out.writeUInt32LE((IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ) >>> 0, headerSize + 36);

  // .rsrc payload
  rsrc.copy(out, rsrcRawPtr);

  // Relocations
  relocs.forEach((va, idx) => {
    const off = relocPtr + idx * 10;
    out.writeUInt32LE(va >>> 0, off);
    out.writeUInt32LE(0, off + 4); // SymbolTableIndex = 0 (.rsrc section symbol)
    out.writeUInt16LE(IMAGE_REL_AMD64_ADDR32NB, off + 8);
  });

  // Symbol Table: .rsrc static section symbol
  out.write(".rsrc", symTabPtr, "ascii");
  out.writeUInt32LE(0, symTabPtr + 8); // Value
  out.writeInt16LE(1, symTabPtr + 12); // SectionNumber (1-based)
  out.writeUInt16LE(0, symTabPtr + 14); // Type
  out.writeUInt8(IMAGE_SYM_CLASS_STATIC, symTabPtr + 16); // StorageClass
  out.writeUInt8(0, symTabPtr + 17); // NumberOfAuxSymbols

  // Empty String Table (length = 4)
  out.writeUInt32LE(4, symTabPtr + symTabSize);

  return out;
}

export function ensureWindowsIconSyso({ icoPath = DEFAULT_ICO, outPath = DEFAULT_SYSO } = {}) {
  assert.ok(existsSync(icoPath), `Windows icon missing at ${icoPath}`);
  const expected = buildIconSysoBuffer(readFileSync(icoPath));
  if (existsSync(outPath) && readFileSync(outPath).equals(expected)) {
    return outPath;
  }
  mkdirSync(dirname(outPath), { recursive: true });
  writeFileSync(outPath, expected);
  return outPath;
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  const out = ensureWindowsIconSyso();
  console.log(`Generated Windows icon resource: ${out}`);
}
