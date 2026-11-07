// 唯一名单：GitHub Release 只放这 5 个安装包。不要 Windows ARM、Linux arm64、rpm、校验和、压缩包或 blockmap。
// The only GitHub Release assets are these 5 installers.
export const INSTALLERS = [
  "block-macos-arm64.dmg",
  "block-macos-x64.dmg",
  "block-windows-x64-setup.exe",
  "block-linux-x64.deb",
  "block-linux-x64.AppImage",
];

export const RELEASE_ASSETS = [...INSTALLERS];

export function assertReleaseAsset(name) {
  if (!RELEASE_ASSETS.includes(name)) {
    throw new Error(`Refusing to upload ${name}; it is not on the release allowlist`);
  }
}

function archLabel(arch) {
  if (arch === "arm64" || arch === "aarch64") return "arm64";
  if (arch === "x64" || arch === "x86_64" || arch === "amd64") return "x64";
  throw new Error(`unsupported arch ${arch}`);
}

const KIND = {
  dmg: (name) => name.endsWith(".dmg"),
  exe: (name) => name.endsWith("-setup.exe"),
  AppImage: (name) => name.endsWith(".AppImage"),
  deb: (name) => name.endsWith(".deb"),
};

export function installerFor({ platform, arch, kind }) {
  if (platform !== "macos" && platform !== "windows" && platform !== "linux") {
    throw new Error(`unsupported platform ${platform}`);
  }
  const pred = KIND[kind];
  if (!pred) throw new Error(`unsupported kind ${kind}`);
  const prefix = `block-${platform}-${archLabel(arch)}`;
  const found = INSTALLERS.filter((name) => (name.startsWith(`${prefix}.`) || name.startsWith(`${prefix}-`)) && pred(name));
  if (found.length !== 1) throw new Error(`allowlist does not name one ${platform} ${arch} ${kind} installer`);
  return found[0];
}

if (import.meta.main) {
  process.stdout.write(`${installerFor({ platform: process.argv[2], arch: process.argv[3], kind: process.argv[4] })}\n`);
}
