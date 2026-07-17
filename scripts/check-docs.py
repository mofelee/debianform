#!/usr/bin/env python3

from __future__ import annotations

import html
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parent.parent
EXCLUDED = {Path("AGENTS.md")}
CHINESE_SUFFIXES = (".zh.md", ".zh-CN.md")
FENCE_RE = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")
HEADING_RE = re.compile(r"^(#{1,6})\s+")
INLINE_LINK_RE = re.compile(r"!?\[[^\]]*\]\((<[^>]+>|[^)\s]+)(?:\s+[^)]*)?\)")
REFERENCE_LINK_RE = re.compile(r"^\s*\[[^\]]+\]:\s*(<[^>]+>|\S+)")
HTML_LINK_RE = re.compile(r"\bhref=[\"']([^\"']+)[\"']", re.IGNORECASE)
HAN_RE = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]")


@dataclass(frozen=True)
class DocumentShape:
    headings: tuple[int, ...]
    fences: tuple[str, ...]
    tables: int
    checklists: int


def relative(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def is_chinese(path: Path) -> bool:
    return path.name.endswith(CHINESE_SUFFIXES)


def counterpart_for(english: Path) -> Path:
    suffix = ".zh-CN.md" if english.parent == ROOT else ".zh.md"
    return english.with_name(english.name[:-3] + suffix)


def maintained_markdown() -> list[Path]:
    paths = []
    for path in ROOT.rglob("*.md"):
        rel = path.relative_to(ROOT)
        if rel in EXCLUDED or ".git" in rel.parts:
            continue
        paths.append(path)
    return sorted(paths)


def parse_shape(path: Path, errors: list[str]) -> DocumentShape:
    headings: list[int] = []
    fences: list[str] = []
    tables = 0
    checklists = 0
    active_fence: tuple[str, int] | None = None

    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        fence_match = FENCE_RE.match(line)
        if active_fence:
            if fence_match:
                marker = fence_match.group(1)
                if marker[0] == active_fence[0] and len(marker) >= active_fence[1]:
                    active_fence = None
            continue
        if fence_match:
            marker = fence_match.group(1)
            active_fence = (marker[0], len(marker))
            fences.append(fence_match.group(2).strip())
            continue
        heading_match = HEADING_RE.match(line)
        if heading_match:
            headings.append(len(heading_match.group(1)))
        if re.match(r"^\s*\|", line):
            tables += 1
        if re.match(r"^\s*[-*]\s+\[[ xX]\]\s+", line):
            checklists += 1

    if active_fence:
        errors.append(f"{relative(path)}: unclosed fenced code block")
    return DocumentShape(tuple(headings), tuple(fences), tables, checklists)


def selector_errors(path: Path, counterpart: Path) -> list[str]:
    errors = []
    head = "\n".join(path.read_text(encoding="utf-8").splitlines()[:20])
    target = re.escape(counterpart.name)
    if not re.search(rf"href=[\"'](?:\./)?{target}[\"']", head):
        errors.append(
            f"{relative(path)}: language selector does not link to {counterpart.name} within the first 20 lines"
        )
    expected = "简体中文" if is_chinese(path) else "English"
    if not re.search(rf"<strong>{expected}</strong>", head):
        errors.append(f"{relative(path)}: language selector does not mark {expected} as current")
    return errors


def local_links(path: Path) -> list[tuple[int, str]]:
    links = []
    active_fence: tuple[str, int] | None = None
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        fence_match = FENCE_RE.match(line)
        if active_fence:
            if fence_match:
                marker = fence_match.group(1)
                if marker[0] == active_fence[0] and len(marker) >= active_fence[1]:
                    active_fence = None
            continue
        if fence_match:
            marker = fence_match.group(1)
            active_fence = (marker[0], len(marker))
            continue
        for match in INLINE_LINK_RE.finditer(line):
            links.append((number, match.group(1)))
        reference = REFERENCE_LINK_RE.match(line)
        if reference:
            links.append((number, reference.group(1)))
        for match in HTML_LINK_RE.finditer(line):
            links.append((number, match.group(1)))
    return links


def resolve_local(source: Path, raw_url: str) -> Path | None:
    url = html.unescape(raw_url.strip().strip("<>"))
    parsed = urlsplit(url)
    if parsed.scheme or parsed.netloc or url.startswith("#") or not parsed.path:
        return None
    decoded = unquote(parsed.path)
    if decoded.startswith("/"):
        return (ROOT / decoded.lstrip("/")).resolve()
    return (source.parent / decoded).resolve()


def prose_without_code(path: Path) -> str:
    text = path.read_text(encoding="utf-8")
    text = re.sub(r"<p\b[^>]*>.*?</p>", "", text, flags=re.IGNORECASE | re.DOTALL)
    output = []
    active_fence: tuple[str, int] | None = None
    for line in text.splitlines():
        fence_match = FENCE_RE.match(line)
        if active_fence:
            if fence_match:
                marker = fence_match.group(1)
                if marker[0] == active_fence[0] and len(marker) >= active_fence[1]:
                    active_fence = None
            continue
        if fence_match:
            marker = fence_match.group(1)
            active_fence = (marker[0], len(marker))
            continue
        output.append(re.sub(r"`+[^`]*`+", "", line))
    return "\n".join(output)


def main() -> int:
    errors: list[str] = []
    paths = maintained_markdown()
    english = [path for path in paths if not is_chinese(path)]
    pairs: dict[Path, Path] = {}

    for path in english:
        counterpart = counterpart_for(path)
        pairs[path.resolve()] = counterpart.resolve()
        if not counterpart.is_file():
            errors.append(f"{relative(path)}: missing counterpart {relative(counterpart)}")

    expected_chinese = {path for path in pairs.values()}
    for path in paths:
        if is_chinese(path) and path.resolve() not in expected_chinese:
            errors.append(f"{relative(path)}: Chinese document has no English counterpart")

    for source_resolved, counterpart_resolved in pairs.items():
        source = Path(source_resolved)
        counterpart = Path(counterpart_resolved)
        if not counterpart.is_file():
            continue
        errors.extend(selector_errors(source, counterpart))
        errors.extend(selector_errors(counterpart, source))
        source_shape = parse_shape(source, errors)
        counterpart_shape = parse_shape(counterpart, errors)
        if source_shape != counterpart_shape:
            errors.append(
                f"{relative(source)} and {relative(counterpart)}: structural mismatch "
                f"(headings {len(source_shape.headings)}/{len(counterpart_shape.headings)}, "
                f"fences {len(source_shape.fences)}/{len(counterpart_shape.fences)}, "
                f"tables {source_shape.tables}/{counterpart_shape.tables}, "
                f"checklists {source_shape.checklists}/{counterpart_shape.checklists})"
            )

    for path in english:
        for line_number, line in enumerate(prose_without_code(path).splitlines(), 1):
            if HAN_RE.search(line):
                errors.append(f"{relative(path)}:{line_number}: unexpected Han text in English prose")

    language_by_path = {path.resolve(): is_chinese(path) for path in paths}
    for path in paths:
        counterpart = pairs.get(path.resolve())
        if is_chinese(path):
            counterpart = next(
                (english_path for english_path, chinese_path in pairs.items() if chinese_path == path.resolve()),
                None,
            )
        for line_number, raw_url in local_links(path):
            target = resolve_local(path, raw_url)
            if target is None:
                continue
            try:
                target.relative_to(ROOT)
            except ValueError:
                errors.append(f"{relative(path)}:{line_number}: local link escapes repository: {raw_url}")
                continue
            if not target.exists():
                errors.append(f"{relative(path)}:{line_number}: local link target does not exist: {raw_url}")
                continue
            if target.suffix != ".md" or target.resolve() not in language_by_path:
                continue
            if counterpart and target.resolve() == counterpart:
                continue
            if language_by_path[target.resolve()] != is_chinese(path):
                errors.append(f"{relative(path)}:{line_number}: cross-language documentation link: {raw_url}")

    if errors:
        print("Documentation validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print(f"Documentation validation passed: {len(pairs)} English/Chinese pairs checked.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
