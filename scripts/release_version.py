#!/usr/bin/env python3
"""Calculate next SemVer release from immutable Git tags and merged commits."""
import argparse
import re
import subprocess
import sys
from pathlib import Path

TAG = re.compile(r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
FEATURE = re.compile(r"^feat(?:\([^)]+\))?!?:", re.I)
BREAKING = re.compile(r"^[a-z][a-z0-9-]*(?:\([^)]+\))?!:", re.I)
FOOTER = re.compile(r"^BREAKING[ -]CHANGE:\s*\S", re.I | re.M)


def git(*args: str, cwd: str | None = None) -> str:
    result = subprocess.run(["git", *args], cwd=cwd, text=True, capture_output=True, check=True)
    return result.stdout.strip()


def latest_version(cwd: str | None = None) -> tuple[str | None, tuple[int, int, int]]:
    valid = []
    for candidate in git("tag", "--list", "v*", cwd=cwd).splitlines():
        match = TAG.fullmatch(candidate)
        if match:
            valid.append((tuple(map(int, match.groups())), candidate))
    if not valid:
        return None, (0, 0, 0)
    version, name = max(valid)
    return name, version


def bump_for_messages(messages: list[str]) -> str:
    level = "patch"
    for message in messages:
        subject = message.splitlines()[0] if message else ""
        if BREAKING.match(subject) or FOOTER.search(message):
            return "major"
        if FEATURE.match(subject):
            level = "minor"
    return level


def next_release(cwd: str | None = None) -> tuple[bool, str, str]:
    tag, (major, minor, patch) = latest_version(cwd)
    if tag:
        # A superseded main-branch run must never release an older commit.
        reachable = subprocess.run(
            ["git", "merge-base", "--is-ancestor", tag, "HEAD"],
            cwd=cwd, capture_output=True, check=False,
        )
        if reachable.returncode == 1:
            return False, f"{major}.{minor}.{patch}", "superseded"
        if reachable.returncode:
            raise RuntimeError(f"unable to verify ancestry of {tag}")
    raw = git("log", "--format=%B%x00", f"{tag}..HEAD" if tag else "HEAD", cwd=cwd)
    messages = [entry.strip() for entry in raw.split("\x00") if entry.strip()]
    if not messages:
        return False, f"{major}.{minor}.{patch}", "already-released"
    level = bump_for_messages(messages)
    if not tag:
        new = (0, 1, 0)
    elif level == "major":
        new = (major + 1, 0, 0)
    elif level == "minor":
        new = (major, minor + 1, 0)
    else:
        new = (major, minor, patch + 1)
    return True, ".".join(map(str, new)), level


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--github-output", type=Path)
    args = parser.parse_args()
    publish, version, reason = next_release()
    result = {"publish": str(publish).lower(), "version": version, "reason": reason}
    for key, val in result.items():
        print(f"{key}={val}")
    if args.github_output:
        with args.github_output.open("a", encoding="utf-8") as output:
            for key, val in result.items():
                output.write(f"{key}={val}\n")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"release-version error: {exc}", file=sys.stderr)
        sys.exit(1)
