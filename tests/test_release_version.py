"""Test the real Git-tag and commit history release algorithm."""
import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path

FILE = Path(__file__).resolve().parents[1] / "scripts" / "release_version.py"
spec = importlib.util.spec_from_file_location("release_version", FILE)
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


def git(cwd: str, *args: str) -> str:
    r = subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True, check=True)
    return r.stdout.strip()


def commit(cwd: str, title: str, body: str = "") -> None:
    Path(cwd, "file").write_text(title, encoding="utf-8")
    git(cwd, "add", "file")
    cmd = ["commit", "-m", title]
    if body:
        cmd.extend(["-m", body])
    git(cwd, *cmd)


class ReleaseVersionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = self.tmp.name
        git(self.repo, "init", "-q")
        git(self.repo, "config", "user.name", "Release Test")
        git(self.repo, "config", "user.email", "test@example.invalid")
        commit(self.repo, "feat: initial release")
        git(self.repo, "tag", "v0.1.0")

    def test_already_released(self):
        self.assertEqual(release.next_release(self.repo), (False, "0.1.0", "already-released"))

    def test_renovate_patch(self):
        commit(self.repo, "chore(deps): update golang to 1.27.4")
        self.assertEqual(release.next_release(self.repo), (True, "0.1.1", "patch"))

    def test_feature_minor(self):
        commit(self.repo, "feat(events): add event type")
        self.assertEqual(release.next_release(self.repo), (True, "0.2.0", "minor"))

    def test_breaking_major(self):
        commit(self.repo, "feat(api)!: change protocol")
        self.assertEqual(release.next_release(self.repo), (True, "1.0.0", "major"))

    def test_breaking_footer(self):
        commit(self.repo, "fix: change payload", "BREAKING CHANGE: schema incompatible")
        self.assertEqual(release.next_release(self.repo), (True, "1.0.0", "major"))

    def test_priority_across_commits(self):
        commit(self.repo, "fix: retry")
        commit(self.repo, "feat: new event")
        self.assertEqual(release.next_release(self.repo), (True, "0.2.0", "minor"))

    def test_numerical_order(self):
        git(self.repo, "tag", "v0.10.1")
        git(self.repo, "tag", "v0.9.9")
        self.assertEqual(release.latest_version(self.repo), ("v0.10.1", (0, 10, 1)))

    def test_superseded_run(self):
        commit(self.repo, "fix: later")
        git(self.repo, "tag", "v0.1.1")
        git(self.repo, "checkout", "-q", "v0.1.0")
        self.assertEqual(release.next_release(self.repo), (False, "0.1.1", "superseded"))

    def test_ignore_prereleases_and_unrelated_tags(self):
        git(self.repo, "tag", "v0.1.0-beta")
        git(self.repo, "tag", "release-99")
        self.assertEqual(release.latest_version(self.repo), ("v0.1.0", (0, 1, 0)))


if __name__ == "__main__":
    unittest.main()
