#!/bin/sh
set -eu

OWNER_REPO="mofelee/debianform"
GITHUB_RELEASE_BASE_URL="https://github.com/${OWNER_REPO}/releases/download"

version=""
prefix=""
bin_dir=""
os_override=""
arch_override=""
dry_run=0
force=0

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --version VERSION  Install a specific version, for example v0.1.0-beta.1.
  --prefix DIR       Install prefix. Defaults to /usr/local when writable, otherwise $HOME/.local.
  --bin-dir DIR      Binary install directory. Defaults to <prefix>/bin.
  --os OS            Override OS detection: linux or darwin.
  --arch ARCH        Override architecture detection: amd64 or arm64.
  --dry-run          Print planned actions without downloading or installing.
  --force            Reinstall even when the target version is already installed.
  -h, --help         Show this help.
EOF
}

log() {
  printf '%s\n' "$*"
}

err() {
  printf 'dbf install: %s\n' "$*" >&2
}

die() {
  err "$*"
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || die "--version requires a value"
      version="$2"
      shift 2
      ;;
    --prefix)
      [ "$#" -ge 2 ] || die "--prefix requires a value"
      prefix="${2%/}"
      shift 2
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] || die "--bin-dir requires a value"
      bin_dir="${2%/}"
      shift 2
      ;;
    --os)
      [ "$#" -ge 2 ] || die "--os requires a value"
      os_override="$2"
      shift 2
      ;;
    --arch)
      [ "$#" -ge 2 ] || die "--arch requires a value"
      arch_override="$2"
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    --force)
      force=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

detect_os() {
  if [ -n "$os_override" ]; then
    raw_os="$os_override"
  else
    raw_os="$(uname -s)"
  fi

  case "$raw_os" in
    Linux|linux)
      printf 'linux\n'
      ;;
    Darwin|darwin)
      printf 'darwin\n'
      ;;
    *)
      die "unsupported OS: $raw_os"
      ;;
  esac
}

detect_arch() {
  if [ -n "$arch_override" ]; then
    raw_arch="$arch_override"
  else
    raw_arch="$(uname -m)"
  fi

  case "$raw_arch" in
    x86_64|amd64)
      printf 'amd64\n'
      ;;
    aarch64|arm64)
      printf 'arm64\n'
      ;;
    *)
      die "unsupported architecture: $raw_arch"
      ;;
  esac
}

default_prefix() {
  if [ "$(id -u)" = "0" ] || [ -w /usr/local ]; then
    printf '/usr/local\n'
  else
    printf '%s/.local\n' "$HOME"
  fi
}

latest_version() {
  if [ -n "${DBF_RELEASE_BASE_URL:-}" ]; then
    die "--version is required when DBF_RELEASE_BASE_URL is set"
  fi

  require_cmd curl
  curl --fail --location --retry 3 --show-error --silent \
    "https://api.github.com/repos/${OWNER_REPO}/releases/latest" |
    awk -F\" '/"tag_name":/ { print $4; exit }'
}

download() {
  url="$1"
  out="$2"

  case "$url" in
    file://*)
      src=${url#file://}
      cp "$src" "$out"
      ;;
    *)
      curl --fail --location --retry 3 --show-error --silent --output "$out" "$url"
      ;;
  esac
}

copy_tree() {
  src="$1"
  dst="$2"

  if [ -d "$src" ]; then
    mkdir -p "$dst"
    tar -C "$src" -cf - . | tar -C "$dst" -xf -
  fi
}

version="${version:-$(latest_version)}"
[ -n "$version" ] || die "could not resolve latest release version"

os="$(detect_os)"
arch="$(detect_arch)"
prefix="${prefix:-$(default_prefix)}"
bin_dir="${bin_dir:-${prefix}/bin}"
share_dir="${prefix}/share/debianform"

artifact="dbf_${version}_${os}_${arch}.tar.gz"
release_base="${DBF_RELEASE_BASE_URL:-${GITHUB_RELEASE_BASE_URL}/${version}}"
artifact_url="${release_base}/${artifact}"
checksums_url="${release_base}/checksums.txt"

if [ "$dry_run" = "1" ]; then
  log "version: ${version}"
  log "platform: ${os}/${arch}"
  log "download: ${artifact_url}"
  log "download: ${checksums_url}"
  log "install binary: ${bin_dir}/dbf"
  log "install data: ${share_dir}"
  exit 0
fi

require_cmd curl
require_cmd tar
require_cmd awk
require_cmd grep
require_cmd chmod
require_cmd mkdir
require_cmd mv

if command -v sha256sum >/dev/null 2>&1; then
  sha256_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  sha256_cmd="shasum -a 256"
else
  die "required command not found: sha256sum or shasum"
fi

if [ -x "${bin_dir}/dbf" ] && [ "$force" = "0" ]; then
  current_version="$("${bin_dir}/dbf" --version 2>/dev/null | awk '{ print $2; exit }' || true)"
  if [ "$current_version" = "$version" ]; then
    log "dbf ${version} is already installed at ${bin_dir}/dbf"
    exit 0
  fi
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/debianform-install.XXXXXX")"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT HUP INT TERM

archive_path="${tmp_dir}/${artifact}"
checksums_path="${tmp_dir}/checksums.txt"
extract_dir="${tmp_dir}/extract"

log "Downloading ${artifact_url}"
download "$artifact_url" "$archive_path"
log "Downloading ${checksums_url}"
download "$checksums_url" "$checksums_path"

expected_line="$(grep " ${artifact}$" "$checksums_path" || true)"
[ -n "$expected_line" ] || die "checksum entry not found for ${artifact}"
expected_sha="$(printf '%s\n' "$expected_line" | awk '{ print $1; exit }')"
actual_sha="$($sha256_cmd "$archive_path" | awk '{ print $1; exit }')"
[ "$expected_sha" = "$actual_sha" ] || die "checksum mismatch for ${artifact}"

mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir"
[ -f "${extract_dir}/dbf" ] || die "archive does not contain dbf"

mkdir -p "$bin_dir" "$share_dir"
install_tmp="${bin_dir}/.dbf.${version}.$$"
cp "${extract_dir}/dbf" "$install_tmp"
chmod 0755 "$install_tmp"
mv "$install_tmp" "${bin_dir}/dbf"

[ -f "${extract_dir}/README.md" ] && cp "${extract_dir}/README.md" "${share_dir}/README.md"
[ -f "${extract_dir}/README.zh-CN.md" ] && cp "${extract_dir}/README.zh-CN.md" "${share_dir}/README.zh-CN.md"
[ -f "${extract_dir}/LICENSE" ] && cp "${extract_dir}/LICENSE" "${share_dir}/LICENSE"
[ -f "${extract_dir}/CHANGELOG.md" ] && cp "${extract_dir}/CHANGELOG.md" "${share_dir}/CHANGELOG.md"
[ -f "${extract_dir}/CHANGELOG.zh-CN.md" ] && cp "${extract_dir}/CHANGELOG.zh-CN.md" "${share_dir}/CHANGELOG.zh-CN.md"
[ -f "${extract_dir}/SECURITY.md" ] && cp "${extract_dir}/SECURITY.md" "${share_dir}/SECURITY.md"
[ -f "${extract_dir}/SECURITY.zh-CN.md" ] && cp "${extract_dir}/SECURITY.zh-CN.md" "${share_dir}/SECURITY.zh-CN.md"
copy_tree "${extract_dir}/docs" "${share_dir}/docs"
copy_tree "${extract_dir}/examples" "${share_dir}/examples"

log "Installed dbf ${version} to ${bin_dir}/dbf"
"${bin_dir}/dbf" version
