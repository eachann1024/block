#!/usr/bin/env bash
# Push packaging/aur to ssh://aur@aur.archlinux.org/block-bin.git.
# Does not upload anything to the GitHub Release.
set -euo pipefail

if [[ -z "${AUR_SSH_PRIVATE_KEY:-}" ]]; then
  echo "AUR_SSH_PRIVATE_KEY is unset; skipping AUR update."
  exit 0
fi

tag="${RELEASE_TAG:?RELEASE_TAG is required}"
pkgver="${tag#v}"
if [[ ! "$pkgver" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Refusing AUR update for unexpected version: ${pkgver}" >&2
  exit 1
fi

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

local_deb="$root/dist/release/block-linux-x64.deb"
if [[ -f "$local_deb" ]]; then
  sha="$(sha256sum "$local_deb" | awk '{print $1}')"
else
  curl -fsSL -o "$work/block-linux-x64.deb" \
    "https://github.com/eachann1024/block/releases/download/${tag}/block-linux-x64.deb"
  sha="$(sha256sum "$work/block-linux-x64.deb" | awk '{print $1}')"
fi

pkgbuild="$root/packaging/aur/PKGBUILD"
srcinfo="$root/packaging/aur/.SRCINFO"

mkdir -p "$work/ssh"
printf '%s\n' "$AUR_SSH_PRIVATE_KEY" > "$work/ssh/aur"
chmod 600 "$work/ssh/aur"
ssh-keyscan -H aur.archlinux.org > "$work/ssh/known_hosts"

export GIT_SSH_COMMAND="ssh -i $work/ssh/aur -o IdentitiesOnly=yes -o UserKnownHostsFile=$work/ssh/known_hosts -o StrictHostKeyChecking=yes"
git clone ssh://aur@aur.archlinux.org/block-bin.git "$work/aur"
if ! git -C "$work/aur" rev-parse --verify HEAD >/dev/null 2>&1; then
  git -C "$work/aur" checkout -B master
fi

pkgrel="1"
remote_epoch="0"
if [[ -f "$work/aur/PKGBUILD" ]]; then
  remote_meta="$(
    node -e '
      const fs = require("node:fs");
      const text = fs.readFileSync(process.argv[1], "utf8");
      const unquote = (raw) => {
        const s = String(raw || "").trim();
        if ((s.startsWith("\"") && s.endsWith("\"")) || (s.startsWith("\x27") && s.endsWith("\x27"))) {
          return s.slice(1, -1);
        }
        return s;
      };
      const readScalar = (name) => {
        const re = new RegExp(`^\\s*${name}=([^#\\n]+)`, "m");
        const m = re.exec(text);
        return m ? unquote(m[1]) : "";
      };
      const epoch = readScalar("epoch") || "0";
      const pkgver = readScalar("pkgver");
      const pkgrel = readScalar("pkgrel");
      const shaMatch = /^\s*sha256sums=\(\s*["\x27]?([0-9a-fA-F]{64})["\x27]?(?=\s|\))/m.exec(text);
      const sha = shaMatch ? shaMatch[1] : "";
      process.stdout.write(`${epoch}\n${pkgver}\n${pkgrel}\n${sha}\n`);
    ' "$work/aur/PKGBUILD"
  )"
  remote_epoch="$(printf '%s\n' "$remote_meta" | sed -n '1p')"
  remote_pkgver="$(printf '%s\n' "$remote_meta" | sed -n '2p')"
  remote_pkgrel="$(printf '%s\n' "$remote_meta" | sed -n '3p')"
  remote_sha="$(printf '%s\n' "$remote_meta" | sed -n '4p')"
  if [[ ! "$remote_epoch" =~ ^[0-9]+$ || ! "$remote_pkgver" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || ! "$remote_pkgrel" =~ ^[0-9]+(\.[0-9]+)?$ || ! "$remote_sha" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "Failed to statically parse remote AUR PKGBUILD metadata (${remote_epoch}:${remote_pkgver}-${remote_pkgrel}); aborting safely." >&2
    exit 1
  fi
  if [[ "$remote_epoch" -gt 0 ]]; then
    echo "Remote AUR PKGBUILD uses epoch=${remote_epoch}; refusing to overwrite without explicit epoch handling." >&2
    exit 1
  fi
  cmp_out="$(node -e '
    const target = [0, ...process.argv[1].split(".").map(Number)];
    const remote = [Number(process.argv[2]), ...process.argv[3].split(".").map(Number)];
    if (target.some(Number.isNaN) || remote.some(Number.isNaN)) process.exit(2);
    for (let i = 0; i < 4; i++) {
      if (target[i] < remote[i]) { console.log("older"); process.exit(0); }
      if (target[i] > remote[i]) { console.log("newer"); process.exit(0); }
    }
    console.log("equal");
  ' "$pkgver" "$remote_epoch" "$remote_pkgver")"
  if [[ "$cmp_out" == "older" ]]; then
    echo "AUR block-bin is already at newer version ${remote_epoch}:${remote_pkgver}-${remote_pkgrel}; refusing to downgrade to ${pkgver}."
    exit 0
  elif [[ "$cmp_out" == "equal" ]]; then
    remote_sha_lc="$(printf '%s' "$remote_sha" | tr '[:upper:]' '[:lower:]')"
    sha_lc="$(printf '%s' "$sha" | tr '[:upper:]' '[:lower:]')"
    if [[ "$remote_sha_lc" == "$sha_lc" ]]; then
      echo "AUR block-bin already matches ${pkgver} (${sha})."
      exit 0
    fi
    base_pkgrel="${remote_pkgrel%%.*}"
    pkgrel="$((base_pkgrel + 1))"
  fi
fi

sed -i -E "s/^pkgver=.*/pkgver=${pkgver}/" "$pkgbuild"
sed -i -E "s/^pkgrel=.*/pkgrel=${pkgrel}/" "$pkgbuild"
sed -i -E "s/^sha256sums=.*/sha256sums=('${sha}')/" "$pkgbuild"
sed -i -E "s/^([[:space:]]*pkgver = ).*/\1${pkgver}/" "$srcinfo"
sed -i -E "s/^([[:space:]]*pkgrel = ).*/\1${pkgrel}/" "$srcinfo"
sed -i -E "s/^([[:space:]]*noextract = ).*/\1block-linux-x64-${pkgver}.deb/" "$srcinfo"
sed -i -E "s|^([[:space:]]*source = )[^:]+(::https://github\.com/eachann1024/block/releases/download/)v[^/]+(/block-linux-x64\.deb)|\1block-linux-x64-${pkgver}.deb\2v${pkgver}\3|" "$srcinfo"
sed -i -E "s/^([[:space:]]*sha256sums = ).*/\1${sha}/" "$srcinfo"

grep -q "pkgver = ${pkgver}" "$srcinfo"
grep -q "pkgrel = ${pkgrel}" "$srcinfo"
grep -q "noextract = block-linux-x64-${pkgver}.deb" "$srcinfo"
grep -q "block-linux-x64-${pkgver}.deb::https://github.com/eachann1024/block/releases/download/v${pkgver}/block-linux-x64.deb" "$srcinfo"
grep -q "sha256sums = ${sha}" "$srcinfo"
cp "$pkgbuild" "$root/packaging/aur/.SRCINFO" "$root/packaging/aur/block-bin.install" "$work/aur/"
git -C "$work/aur" add PKGBUILD .SRCINFO block-bin.install
if git -C "$work/aur" diff --cached --quiet; then
  echo "AUR block-bin already matches ${pkgver}."
  exit 0
fi
git -C "$work/aur" -c user.name='block-release' -c user.email='block@cooool.top' commit -m "Update to ${pkgver}"
branch="$(git -C "$work/aur" rev-parse --abbrev-ref HEAD)"
git -C "$work/aur" push origin "$branch"
echo "Pushed block-bin ${pkgver} to AUR."
