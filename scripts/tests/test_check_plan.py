import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "scripts" / "check-plan.py"


class PlanCheckerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "docs").mkdir()
        shutil.copytree(REPO / "docs" / "tasks", self.root / "docs" / "tasks")
        shutil.copytree(REPO / "docs" / "evidence", self.root / "docs" / "evidence")
        shutil.copy(REPO / "docs" / "execution-index.json", self.root / "docs")
        shutil.copy(REPO / "docs" / "usecases.json", self.root / "docs")
        shutil.copy(REPO / "docs" / "plan.md", self.root / "docs")

    def tearDown(self):
        self.temp.cleanup()

    def index(self):
        path = self.root / "docs" / "execution-index.json"
        return json.loads(path.read_text(encoding="utf-8")), path

    def save_index(self, data, path):
        path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")

    def run_checker(self, *args):
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--root", str(self.root), *args],
            text=True,
            capture_output=True,
            check=False,
        )

    def test_current_plan_passes_and_preserves_execution_status(self):
        before = (self.root / "docs" / "execution-index.json").read_bytes()
        data, _ = self.index()
        self.assertTrue(next(t for t in data["tasks"] if t["id"] == "T1.1")["certification"].startswith("Certified:"))
        self.assertTrue(all(t["certification"] == "not_run" for t in data["tasks"] if t["id"] != "T1.1"))
        result = self.run_checker()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(
            (self.root / "docs" / "execution-index.json").read_bytes(), before
        )

    def test_dependency_cycle(self):
        data, path = self.index()
        data["tasks"][0]["deps"] = ["T1.2"]
        self.save_index(data, path)
        result = self.run_checker()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.1", result.stdout + result.stderr)
        self.assertIn("T1.2", result.stdout + result.stderr)
        self.assertIn("dependency cycle", result.stdout + result.stderr)

    def test_missing_contract(self):
        (self.root / "docs" / "tasks" / "T1.2.md").unlink()
        result = self.run_checker()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.2", result.stdout + result.stderr)

    def test_duplicate_task_id(self):
        data, path = self.index()
        data["tasks"].append(data["tasks"][0].copy())
        self.save_index(data, path)
        result = self.run_checker()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.1", result.stdout + result.stderr)

    def test_overlap(self):
        data, path = self.index()
        task = next(item for item in data["tasks"] if item["id"] == "T1.5")
        task["owned_paths"].append("internal/domain/types.go")
        self.save_index(data, path)
        contract = self.root / "docs" / "tasks" / "T1.5.md"
        text = contract.read_text(encoding="utf-8")
        text = text.replace("\n\nOut of scope:", "\n- `internal/domain/types.go`\n\nOut of scope:", 1)
        contract.write_text(text, encoding="utf-8")
        result = self.run_checker()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.2", result.stdout + result.stderr)
        self.assertIn("T1.5", result.stdout + result.stderr)
        self.assertIn("T1.2", result.stdout + result.stderr)

    def test_compatibility_rejects_missing_source_date(self):
        source_dir = self.root / "docs" / "research"
        source_dir.mkdir()
        (source_dir / "vendor-methods.json").write_text(
            json.dumps({"methods": [{"method": "official installer", "platform": "macOS", "source_url": "https://example.test/docs", "evidence_level": "documented_only"}]}),
            encoding="utf-8",
        )
        (self.root / "docs" / "compatibility.md").write_text("compatibility fixture\n", encoding="utf-8")
        result = self.run_checker("--compatibility")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.3", result.stdout + result.stderr)
        self.assertIn("source_date", result.stdout + result.stderr)

    def test_content_rejects_unclassified_entry(self):
        source_dir = self.root / "docs" / "research"
        source_dir.mkdir()
        (source_dir / "content-sources.json").write_text(
            json.dumps({"assets": [{"source_date": "2026-09-28", "decision": "pending"}]}),
            encoding="utf-8",
        )
        (self.root / "docs" / "content-inventory.md").write_text("content fixture\n", encoding="utf-8")
        result = self.run_checker("--content")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.4", result.stdout + result.stderr)
        self.assertIn("classified", result.stdout + result.stderr)

    def test_missing_evidence(self):
        (self.root / "docs" / "evidence" / "T1.1.md").unlink()
        result = self.run_checker()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("T1.1", result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
