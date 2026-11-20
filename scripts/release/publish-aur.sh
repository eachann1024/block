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
sed -i -E "s/^pkgver=.*/pkgver=${pkgver}/" "$pkgbuild"
sed -i -E "s/^sha256sums=.*/sha256sums=('${sha}')/" "$pkgbuild"
sed -i -E "s/^([[:space:]]*pkgver = ).*/\1${pkgver}/" "$srcinfo"
sed -i -E "s/^([[:space:]]*noextract = ).*/\1block-linux-x64-${pkgver}.deb/" "$srcinfo"
sed -i -E "s|^([[:space:]]*source = )[^:]+(::https://github\.com/eachann1024/block/releases/download/)v[^/]+(/block-linux-x64\.deb)|\1block-linux-x64-${pkgver}.deb\2v${pkgver}\3|" "$srcinfo"
sed -i -E "s/^([[:space:]]*sha256sums = ).*/\1${sha}/" "$srcinfo"

grep -q "pkgver = ${pkgver}" "$srcinfo"
grep -q "noextract = block-linux-x64-${pkgver}.deb" "$srcinfo"
grep -q "block-linux-x64-${pkgver}.deb::https://github.com/eachann1024/block/releases/download/v${pkgver}/block-linux-x64.deb" "$srcinfo"
grep -q "sha256sums = ${sha}" "$srcinfo"

mkdir -p "$work/ssh"
printf '%s\n' "$AUR_SSH_PRIVATE_KEY" > "$work/ssh/aur"
chmod 600 "$work/ssh/aur"
ssh-keyscan -H aur.archlinux.org > "$work/ssh/known_hosts"

export GIT_SSH_COMMAND="ssh -i $work/ssh/aur -o IdentitiesOnly=yes -o UserKnownHostsFile=$work/ssh/known_hosts -o StrictHostKeyChecking=yes"
git clone ssh://aur@aur.archlinux.org/block-bin.git "$work/aur"
if ! git -C "$work/aur" rev-parse --verify HEAD >/dev/null 2>&1; then
  git -C "$work/aur" checkout -B master
fi
if [[ -f "$work/aur/PKGBUILD" ]]; then
  remote_pkgver="$(sed -n -E 's/^pkgver=([0-9]+\.[0-9]+\.[0-9]+).*/\1/p' "$work/aur/PKGBUILD" | head -n 1)"
  if [[ -n "$remote_pkgver" ]]; then
    cmp_rc=0
    node -e '
      const a = process.argv[1].split(".").map(Number);
      const b = process.argv[2].split(".").map(Number);
      for (let i = 0; i < 3; i++) {
        if (a[i] !== b[i]) process.exit(a[i] < b[i] ? 10 : 0);
      }
      process.exit(0);
    ' "$pkgver" "$remote_pkgver" || cmp_rc=$?
    if [[ "$cmp_rc" -eq 10 ]]; then
      echo "AUR block-bin is already at newer version ${remote_pkgver}; refusing to downgrade to ${pkgver}."
      exit 0
    fi
  fi
fi
cp "$pkgbuild" "$root/packaging/aur/.SRCINFO" "$work/aur/"
git -C "$work/aur" add PKGBUILD .SRCINFO
if git -C "$work/aur" diff --cached --quiet; then
  echo "AUR block-bin already matches ${pkgver}."
  exit 0
fi
git -C "$work/aur" -c user.name='block-release' -c user.email='block@cooool.top' commit -m "Update to ${pkgver}"
branch="$(git -C "$work/aur" rev-parse --abbrev-ref HEAD)"
git -C "$work/aur" push origin "$branch"
echo "Pushed block-bin ${pkgver} to AUR."
