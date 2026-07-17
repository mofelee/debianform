#!/bin/sh
set -eu

owner_repo="${OWNER_REPO:-mofelee/debianform}"
tap_repo="${HOMEBREW_TAP_REPO:-mofelee/homebrew-debianform}"
tap_dir="${HOMEBREW_TAP_DIR:-}"
tag="${1:-${GITHUB_REF_NAME:-}}"
checksums_file="${2:-dist/checksums.txt}"

die() {
  printf 'update-homebrew-formula: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: update-homebrew-formula.sh <tag> [checksums.txt]

Environment:
  HOMEBREW_TAP_GITHUB_TOKEN  Token used to push mofelee/homebrew-debianform.
  HOMEBREW_TAP_DIR           Existing tap checkout for local tests.
  HOMEBREW_TAP_REPO          Tap repository, defaults to mofelee/homebrew-debianform.
  OWNER_REPO                 Release repository, defaults to mofelee/debianform.
EOF
}

if [ "${tag}" = "-h" ] || [ "${tag}" = "--help" ]; then
  usage
  exit 0
fi

[ -n "$tag" ] || die "tag is required"
[ -s "$checksums_file" ] || die "checksums file not found: $checksums_file"

case "$tag" in
  v*) version="${tag#v}" ;;
  *) version="$tag" ;;
esac

artifact_name() {
  os="$1"
  arch="$2"
  printf 'dbf_%s_%s_%s.tar.gz' "$tag" "$os" "$arch"
}

sha_for() {
  artifact="$1"
  awk -v artifact="$artifact" '$2 == artifact { print $1; found = 1 } END { if (!found) exit 1 }' "$checksums_file" ||
    die "checksum entry not found for $artifact"
}

darwin_amd64_artifact="$(artifact_name darwin amd64)"
darwin_arm64_artifact="$(artifact_name darwin arm64)"
linux_amd64_artifact="$(artifact_name linux amd64)"
linux_arm64_artifact="$(artifact_name linux arm64)"

darwin_amd64_sha="$(sha_for "$darwin_amd64_artifact")"
darwin_arm64_sha="$(sha_for "$darwin_arm64_artifact")"
linux_amd64_sha="$(sha_for "$linux_amd64_artifact")"
linux_arm64_sha="$(sha_for "$linux_arm64_artifact")"

cleanup=""
if [ -n "$tap_dir" ]; then
  [ -d "$tap_dir/.git" ] || die "HOMEBREW_TAP_DIR is not a git checkout: $tap_dir"
else
  [ -n "${HOMEBREW_TAP_GITHUB_TOKEN:-}" ] || die "HOMEBREW_TAP_GITHUB_TOKEN is required"
  tap_dir="$(mktemp -d "${TMPDIR:-/tmp}/homebrew-debianform.XXXXXX")"
  cleanup="$tap_dir"
  git clone "https://x-access-token:${HOMEBREW_TAP_GITHUB_TOKEN}@github.com/${tap_repo}.git" "$tap_dir"
fi

finish() {
  if [ -n "$cleanup" ]; then
    rm -rf "$cleanup"
  fi
}
trap finish EXIT HUP INT TERM

formula_dir="$tap_dir/Formula"
formula_path="$formula_dir/dbf.rb"
mkdir -p "$formula_dir"

cat > "$formula_path" <<EOF
class Dbf < Formula
  desc "Configuration tool for Debian hosts"
  homepage "https://github.com/${owner_repo}"
  version "${version}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/${owner_repo}/releases/download/${tag}/${darwin_arm64_artifact}"
      sha256 "${darwin_arm64_sha}"
    else
      url "https://github.com/${owner_repo}/releases/download/${tag}/${darwin_amd64_artifact}"
      sha256 "${darwin_amd64_sha}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/${owner_repo}/releases/download/${tag}/${linux_arm64_artifact}"
      sha256 "${linux_arm64_sha}"
    else
      url "https://github.com/${owner_repo}/releases/download/${tag}/${linux_amd64_artifact}"
      sha256 "${linux_amd64_sha}"
    end
  end

  def install
    bin.install "dbf"
    pkgshare.install "README.md", "README.zh-CN.md", "CHANGELOG.md", "CHANGELOG.zh-CN.md", "SECURITY.md", "SECURITY.zh-CN.md", "docs", "examples"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/dbf version")
  end
end
EOF

if [ -n "$cleanup" ]; then
  cd "$tap_dir"
  git config user.name "${GIT_COMMITTER_NAME:-github-actions[bot]}"
  git config user.email "${GIT_COMMITTER_EMAIL:-41898282+github-actions[bot]@users.noreply.github.com}"
  git add Formula/dbf.rb
  if git diff --cached --quiet; then
    printf 'Homebrew formula already up to date for %s\n' "$tag"
  else
    git commit -m "dbf ${tag}"
    git push origin HEAD:main
  fi
else
  printf 'Updated %s for %s\n' "$formula_path" "$tag"
fi
