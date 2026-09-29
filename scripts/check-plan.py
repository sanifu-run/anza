#!/usr/bin/env python3
"""Validate Anza's execution index and task metadata without changing it."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import sys
from collections import defaultdict
from pathlib import Path, PurePosixPath


TASK_ID = re.compile(r"^T\d+\.\d+$")
USE_CASE_ID = re.compile(r"^UC-\d{3}$")
DATE = re.compile(r"^\d{4}-\d{2}-\d{2}(?:T.*Z)?$")


class Validation:
    def __init__(self) -> None:
        self.errors: list[str] = []

    def error(self, identity: str, message: str) -> None:
        self.errors.append(f"ERROR [{identity}] {message}")


def read_json(path: Path, check: Validation, identity: str):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        check.error(identity, f"missing required file: {path}")
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        check.error(identity, f"cannot read valid JSON from {path}: {exc}")
    return None


def task_doc_paths(path: Path) -> tuple[str | None, list[str]]:
    text = path.read_text(encoding="utf-8")
    repo_match = re.search(r"^Repository:\s*`([^`]+)`", text, re.MULTILINE)
    scope = re.search(
        r"^## Scope\s*$(.*?)(?=^## |\Z)", text, re.MULTILINE | re.DOTALL
    )
    paths = []
    if scope:
        paths = re.findall(r"^\s*-\s*`([^`]+)`\s*$", scope.group(1), re.MULTILINE)
    return (repo_match.group(1) if repo_match else None), paths


def valid_relative_path(value: object) -> bool:
    if not isinstance(value, str) or not value or "\\" in value:
        return False
    path = PurePosixPath(value)
    return not path.is_absolute() and all(part not in ("", ".", "..") for part in value.split("/"))


def validate_source_mode(root: Path, check: Validation, mode: str) -> None:
    if mode == "compatibility":
        metadata = root / "docs/research/vendor-methods.json"
        companion = root / "docs/compatibility.md"
        identity = "T1.3"
        collection_names = ("methods", "sources", "entries")
        required = ("method", "platform", "source_url", "source_date", "evidence_level")
    else:
        metadata = root / "docs/research/content-sources.json"
        companion = root / "docs/content-inventory.md"
        identity = "T1.4"
        collection_names = ("assets", "sources", "entries")
        required = ("source_date", "decision")

    if not metadata.is_file():
        check.error(identity, f"--{mode} requires {metadata}")
    if not companion.is_file():
        check.error(identity, f"--{mode} requires {companion}")
    if not metadata.is_file():
        return

    data = read_json(metadata, check, identity)
    if data is None:
        return
    rows = data if isinstance(data, list) else next(
        (data.get(key) for key in collection_names if isinstance(data, dict) and key in data),
        None,
    )
    if not isinstance(rows, list) or not rows:
        check.error(identity, f"{metadata} must contain a non-empty list of metadata entries")
        return

    allowed_decisions = {"keep", "adapt", "exclude"}
    for number, row in enumerate(rows, 1):
        row_id = row.get("id") if isinstance(row, dict) else None
        label = f"{identity}:{row_id or number}"
        if not isinstance(row, dict):
            check.error(label, "metadata entry must be an object")
            continue
        for field in required:
            if not isinstance(row.get(field), str) or not row[field].strip():
                check.error(label, f"missing or empty {field}")
        date_value = row.get("source_date")
        if isinstance(date_value, str) and date_value.strip():
            if not DATE.fullmatch(date_value):
                check.error(label, "source_date must be an ISO calendar date or UTC RFC3339 date")
            else:
                try:
                    if len(date_value) == 10:
                        dt.date.fromisoformat(date_value)
                    else:
                        parsed = dt.datetime.fromisoformat(date_value[:-1] + "+00:00")
                        if parsed.utcoffset() != dt.timedelta(0):
                            check.error(label, "source_date timestamp must be UTC")
                except ValueError:
                    check.error(label, f"invalid source_date {date_value!r}")
        if mode == "compatibility":
            if row.get("evidence_level") not in {
                "documented_only", "local_test", "native_test", "live_verified", "manual"
            }:
                check.error(label, "evidence_level is not a recognized classification")
            if not isinstance(row.get("source_url"), str) or not row.get("source_url", "").startswith("https://"):
                check.error(label, "source_url must be an HTTPS URL")
        elif row.get("decision") not in allowed_decisions:
            check.error(label, "decision must be classified as keep, adapt, or exclude")


def validate(root: Path, modes: set[str]) -> list[str]:
    check = Validation()
    docs = root / "docs"
    index_path = docs / "execution-index.json"
    index = read_json(index_path, check, "execution-index")
    if not isinstance(index, dict):
        return check.errors
    tasks = index.get("tasks")
    if not isinstance(tasks, list):
        check.error("execution-index", "tasks must be a list")
        tasks = []

    by_id: dict[str, dict] = {}
    for number, task in enumerate(tasks, 1):
        if not isinstance(task, dict):
            check.error(f"task#{number}", "task entry must be an object")
            continue
        task_id = task.get("id")
        if not isinstance(task_id, str) or not TASK_ID.fullmatch(task_id):
            check.error(str(task_id or f"task#{number}"), "invalid or missing task ID")
            continue
        if task_id in by_id:
            check.error(task_id, "duplicate task ID")
            continue
        by_id[task_id] = task

    repositories = index.get("repositories")
    if not isinstance(repositories, dict) or not {"anza", "chat"}.issubset(repositories):
        check.error("execution-index", "repositories must define both anza and chat")

    wave_data = index.get("waves")
    waves: dict[int, dict] = {}
    listed_tasks: dict[str, int] = {}
    if not isinstance(wave_data, list):
        check.error("execution-index", "waves must be a list")
        wave_data = []
    for wave in wave_data:
        if not isinstance(wave, dict) or not isinstance(wave.get("id"), int):
            check.error("execution-index", "wave entries need an integer id")
            continue
        wave_id = wave["id"]
        if wave_id in waves:
            check.error(f"wave {wave_id}", "duplicate wave ID")
        waves[wave_id] = wave
        ids = wave.get("tasks")
        if not isinstance(ids, list):
            check.error(f"wave {wave_id}", "tasks must be a list")
            continue
        for task_id in ids:
            if task_id not in by_id:
                check.error(str(task_id), f"listed in unknown/malformed wave {wave_id}")
                continue
            if task_id in listed_tasks:
                check.error(task_id, f"listed in more than one wave ({listed_tasks[task_id]}, {wave_id})")
            listed_tasks[task_id] = wave_id
            if by_id[task_id].get("wave") != wave_id:
                check.error(task_id, f"task wave does not match wave {wave_id}")
    for task_id in by_id:
        if task_id not in listed_tasks:
            check.error(task_id, "missing from wave schedule")

    usecase_data = read_json(docs / "usecases.json", check, "usecases")
    usecases: dict[str, dict] = {}
    if isinstance(usecase_data, list):
        for row in usecase_data:
            if not isinstance(row, dict) or not isinstance(row.get("id"), str):
                check.error("usecases", "each use case must have an ID")
                continue
            if row["id"] in usecases:
                check.error(row["id"], "duplicate use-case ID")
            usecases[row["id"]] = row
    else:
        check.error("usecases", "canonical usecases.json must be a list")

    owners_by_wave: dict[tuple[str, int], list[tuple[str, str]]] = defaultdict(list)
    graph: dict[str, list[str]] = {}
    for task_id, task in by_id.items():
        repo = task.get("repository")
        if repo not in {"anza", "chat"}:
            check.error(task_id, f"repository must be anza or chat, got {repo!r}")
        owned = task.get("owned_paths")
        if not isinstance(owned, list) or not owned:
            check.error(task_id, "owned_paths must be a non-empty list")
            owned = []
        if len(owned) != len(set(item for item in owned if isinstance(item, str))):
            check.error(task_id, "duplicate path in owned_paths")
        for path in owned:
            if not valid_relative_path(path):
                check.error(task_id, f"owned path must be repository-relative: {path!r}")
            elif repo in {"anza", "chat"}:
                owners_by_wave[(repo, task.get("wave", -1))].append((path, task_id))

        doc_path = docs / "tasks" / f"{task_id}.md"
        if not doc_path.is_file():
            check.error(task_id, f"missing task contract {doc_path}")
        else:
            try:
                doc_repo, doc_owned = task_doc_paths(doc_path)
            except (OSError, UnicodeError) as exc:
                check.error(task_id, f"cannot read task contract: {exc}")
                doc_repo, doc_owned = None, []
            if doc_repo != repo:
                check.error(task_id, f"task contract repository {doc_repo!r} does not match index {repo!r}")
            if doc_owned != owned:
                check.error(task_id, "task contract owned paths do not match execution index")

        cases = task.get("use_cases")
        if not isinstance(cases, list) or not cases:
            check.error(task_id, "use_cases must be a non-empty list")
            cases = []
        for case_id in cases:
            if case_id != "infrastructure" and case_id not in usecases:
                check.error(task_id, f"references unknown use case {case_id!r}")

        deps = task.get("deps")
        if not isinstance(deps, list):
            check.error(task_id, "deps must be a list")
            deps = []
        graph[task_id] = deps
        for dep in deps:
            if dep not in by_id:
                check.error(task_id, f"dependency {dep!r} does not exist")
            elif dep == task_id:
                check.error(task_id, "task cannot depend on itself")
            elif dep in listed_tasks and task_id in listed_tasks and listed_tasks[dep] >= listed_tasks[task_id]:
                check.error(task_id, f"dependency {dep} must run in an earlier wave")

    for (repo, wave), claims in owners_by_wave.items():
        conflicts: set[tuple[str, str, str]] = set()
        for index, (path_a, task_a) in enumerate(claims):
            for path_b, task_b in claims[index + 1:]:
                if task_a != task_b and (
                    path_a == path_b
                    or path_a.startswith(path_b + "/")
                    or path_b.startswith(path_a + "/")
                ):
                    conflicts.add((path_a, path_b, ", ".join(sorted((task_a, task_b)))))
        for path_a, path_b, task_ids in sorted(conflicts):
            check.error(task_ids, f"overlapping concurrent output {repo}:{path_a} and {path_b} in wave {wave}")

    state: dict[str, int] = {}
    stack: list[str] = []

    def visit(task_id: str) -> None:
        state[task_id] = 1
        stack.append(task_id)
        for dep in graph.get(task_id, []):
            if dep not in graph:
                continue
            if state.get(dep) == 1:
                cycle = stack[stack.index(dep):] + [dep]
                check.error(" -> ".join(cycle), "dependency cycle")
            elif state.get(dep, 0) == 0:
                visit(dep)
        stack.pop()
        state[task_id] = 2

    for task_id in graph:
        if state.get(task_id, 0) == 0:
            visit(task_id)

    # The canonical use-case map and task index must agree in both directions.
    for case_id, row in usecases.items():
        case_tasks = row.get("task_ids")
        if not isinstance(case_tasks, list):
            check.error(case_id, "task_ids must be a list")
            continue
        for task_id in case_tasks:
            if task_id not in by_id:
                check.error(case_id, f"maps to unknown task {task_id!r}")
            elif case_id not in by_id[task_id].get("use_cases", []):
                check.error(task_id, f"use-case mapping {case_id} is missing from execution index")
    for task_id, task in by_id.items():
        for case_id in task.get("use_cases", []):
            if case_id in usecases and task_id not in usecases[case_id].get("task_ids", []):
                check.error(task_id, f"use-case mapping {case_id} is missing from canonical usecases.json")

    validate_execution_evidence(root, by_id, check)
    for mode in modes:
        validate_source_mode(root, check, mode)
    return check.errors


def validate_execution_evidence(root: Path, tasks: dict[str, dict], check: Validation) -> None:
    for task_id, task in tasks.items():
        certification = task.get("certification")
        if certification == "not_run":
            continue
        if not isinstance(certification, str) or not certification.startswith("Certified: GPT-6-Luna "):
            check.error(task_id, f"invalid certification state {certification!r}")
            continue
        if task.get("repository") != "anza":
            # Chat evidence remains in its repository and is reviewed at integration.
            continue
        evidence = root / "docs" / "evidence" / f"{task_id}.md"
        if not evidence.is_file():
            check.error(task_id, f"certified task is missing evidence file {evidence}")
            continue
        try:
            text = evidence.read_text(encoding="utf-8")
        except (OSError, UnicodeError) as exc:
            check.error(task_id, f"cannot read evidence: {exc}")
            continue
        if not text.strip() or task_id not in text or not re.search(r"(?i)(test|check|command)", text) or not re.search(r"(?i)(pass|observed|result)", text):
            check.error(task_id, "evidence must identify the task and record checks with observed results")

    plan = root / "docs" / "plan.md"
    try:
        plan_text = plan.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        check.error("plan", f"cannot read docs/plan.md: {exc}")
        return
    checkboxes = re.findall(r"^- \[([ xX])\] (T\d+\.\d+)\b", plan_text, re.MULTILINE)
    plan_states: dict[str, list[bool]] = defaultdict(list)
    for mark, task_id in checkboxes:
        plan_states[task_id].append(mark.lower() == "x")
    for task_id, task in tasks.items():
        certification = task.get("certification")
        expected_done = isinstance(certification, str) and certification.startswith("Certified: GPT-6-Luna ")
        states = plan_states.get(task_id, [])
        if len(states) != 1:
            check.error(task_id, f"plan must contain exactly one checklist row; found {len(states)}")
        elif states[0] != expected_done:
            check.error(task_id, "plan checklist status does not match coordinator certification")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--compatibility", action="store_true", help="validate T1.3 source metadata")
    parser.add_argument("--content", action="store_true", help="validate T1.4 provenance metadata")
    parser.add_argument("--root", type=Path, default=Path.cwd(), help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    modes = set()
    if args.compatibility:
        modes.add("compatibility")
    if args.content:
        modes.add("content")
    errors = validate(args.root.resolve(), modes)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        print(f"Plan validation failed with {len(errors)} error(s).", file=sys.stderr)
        return 1
    print("Plan validation passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
