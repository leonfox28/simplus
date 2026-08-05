#!/usr/bin/env python3

"""Validate the small, navigable repository documentation map."""

from __future__ import annotations

from pathlib import Path
import re
import sys


REQUIRED_PATHS = (
    Path("README.md"),
    Path("LICENSE"),
    Path("THIRD_PARTY_NOTICES.md"),
    Path("AGENTS.md"),
    Path("docs/README.md"),
    Path("docs/product.md"),
    Path("docs/architecture.md"),
    Path("docs/handoff.zh-CN.md"),
    Path("docs/development.md"),
    Path("docs/compatibility.md"),
    Path("docs/troubleshooting.md"),
    Path("docs/privacy-and-publication.md"),
    Path("docs/decisions/0001-product-scope-reset.md"),
    Path("docs/decisions/0002-restore-vowifi-mihomo-euicc.md"),
)
LINK_PATTERN = re.compile(r"(?<!!)\[[^\]]*\]\(([^)]+)\)")
EXTERNAL_PREFIXES = ("#", "http://", "https://", "mailto:")
PRIVATE_DOC_ROOTS = (
    Path("docs/archive"),
    Path("docs/hardware"),
    Path("docs/private"),
)
RETIRED_HANDOFF_MARKERS = re.compile(
    r"SIMPLUS_DEV_|scripts/dev/remote\.sh|sync-to-host\.sh|simplus-dev|rsync",
    re.IGNORECASE,
)
PUBLIC_CONTENT_RULES = (
    (
        "RFC1918 address",
        re.compile(
            r"\b(?:10(?:\.[0-9]{1,3}){3}|192\.168(?:\.[0-9]{1,3}){2}|"
            r"172\.(?:1[6-9]|2[0-9]|3[01])(?:\.[0-9]{1,3}){2})\b"
        ),
    ),
    ("personal home path", re.compile(r"/(?:home|Users)/[A-Za-z0-9._-]+(?:/|\b)")),
    ("credential-bearing URL", re.compile(r"https?://[^\s/@]+:[^\s/@]+@", re.IGNORECASE)),
    (
        "proxy share URI",
        re.compile(r"\b(?:vless|trojan|tuic|hysteria2?|ss)://[^\s]+", re.IGNORECASE),
    ),
    (
        "credential query parameter",
        re.compile(r"[?&](?:token|password|secret|key)=[^<`\s&)]+", re.IGNORECASE),
    ),
    (
        "telecom identity value",
        re.compile(
            r"\b(?:imsi|iccid|imei|eid|impi|impu|msisdn)\b[^\n]{0,40}\d{10,}",
            re.IGNORECASE,
        ),
    ),
    (
        "private-site marker",
        re.compile(r"\b(?:localadmin|huyuanzhe|MikroTik|OpenClash|VOXI)\b", re.IGNORECASE),
    ),
)


def fail(message: str) -> None:
    print(f"documentation check failed: {message}", file=sys.stderr)


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    errors: list[str] = []

    for relative in REQUIRED_PATHS:
        if not (root / relative).is_file():
            errors.append(f"required document is missing: {relative}")

    active_plans = sorted((root / "docs/plans/active").glob("*.md"))
    if len(active_plans) != 1:
        errors.append(f"expected exactly one active plan, found {len(active_plans)}")

    for relative in PRIVATE_DOC_ROOTS:
        private_root = root / relative
        if private_root.exists() and any(private_root.rglob("*.md")):
            errors.append(f"private record directory is present in the public tree: {relative}")

    agents = root / "AGENTS.md"
    if agents.is_file():
        line_count = len(agents.read_text(encoding="utf-8").splitlines())
        if line_count > 100:
            errors.append(f"AGENTS.md is a manual instead of a map: {line_count} lines")

    handoff = root / "docs/handoff.zh-CN.md"
    if handoff.is_file():
        handoff_text = handoff.read_text(encoding="utf-8")
        for match in RETIRED_HANDOFF_MARKERS.finditer(handoff_text):
            line = handoff_text.count("\n", 0, match.start()) + 1
            errors.append(
                f"docs/handoff.zh-CN.md:{line} contains a retired remote-development marker"
            )

    markdown_files = [
        root / "README.md",
        root / "THIRD_PARTY_NOTICES.md",
        root / "AGENTS.md",
        *(root / "docs").rglob("*.md"),
    ]
    for source in markdown_files:
        if not source.is_file():
            continue
        text = source.read_text(encoding="utf-8")
        for rule_name, pattern in PUBLIC_CONTENT_RULES:
            for match in pattern.finditer(text):
                line = text.count("\n", 0, match.start()) + 1
                errors.append(
                    f"{source.relative_to(root)}:{line} contains prohibited public content: {rule_name}"
                )
        for match in LINK_PATTERN.finditer(text):
            target = match.group(1).strip()
            if not target or target.startswith(EXTERNAL_PREFIXES):
                continue
            relative_target = target.split("#", 1)[0]
            resolved = (source.parent / relative_target).resolve()
            try:
                resolved.relative_to(root)
            except ValueError:
                errors.append(f"{source.relative_to(root)} links outside the repository: {target}")
                continue
            if not resolved.exists():
                errors.append(f"{source.relative_to(root)} has a missing link: {target}")

    if errors:
        for error in errors:
            fail(error)
        return 1

    print(
        f"documentation checks passed: {len(markdown_files)} Markdown files, "
        f"active plan {active_plans[0].relative_to(root)}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
