#!/usr/bin/env python3
"""Disposable MemoryCore experiment. No sous command, plugin, or personal data."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, build_opener, ProxyHandler

UPSTREAM_COMMIT = "ecc01a545da0b2ac6cae2f0f0af0d39d41469793"
SOURCE = re.compile(r"\[sous-source:([a-z0-9-]+)\]")


class MemoryUnavailable(RuntimeError):
    pass


def write_json(path, value):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(path)


def load_corpus(path):
    corpus = json.loads(path.read_text())
    if corpus.get("version") != 1:
        raise ValueError("unsupported corpus version")
    ids = [r["id"] for r in corpus["records"]]
    if len(ids) != len(set(ids)):
        raise ValueError("duplicate source id")
    return corpus


def scope(workspace, project):
    """Logical project identity survives changes of agent tool and session."""
    digest = lambda value: hashlib.sha256(value.encode()).hexdigest()[:20]
    return {"team_id": "workspace-" + digest(workspace),
            "agent_id": "project-" + digest(workspace + "\0" + project),
            "user_id": "sous-experiment"}


def baseline_search(corpus, query, workspace, projects, limit=3):
    """Transparent baseline: unique keyword overlap, with newest breaking ties."""
    words = set(re.findall(r"[a-z0-9]+", query.lower()))
    matches = []
    for record in corpus["records"]:
        if record["workspace"] != workspace or record["project"] not in projects:
            continue
        score = len(words & set(re.findall(r"[a-z0-9]+", record["text"].lower())))
        if score:
            matches.append(dict(record, score=score))
    return sorted(matches, key=lambda r: (r["score"], r["date"]), reverse=True)[:limit]


class MemoryCore:
    def __init__(self, endpoint, token):
        self.endpoint, self.token = endpoint, token
        self.opener = build_opener(ProxyHandler({}))

    def post(self, path, body):
        request = Request(self.endpoint + path, data=json.dumps(body).encode(), headers={
            "Content-Type": "application/json", "Authorization": "Bearer " + self.token,
            "x-tdai-service-id": "default"})
        try:
            with self.opener.open(request, timeout=10) as response:
                payload = json.load(response)
            if not isinstance(payload, dict) or payload.get("code") != 0 or "data" not in payload:
                raise MemoryUnavailable("MemoryCore returned an error envelope")
            return payload["data"]
        except (HTTPError, URLError, OSError, ValueError) as error:
            raise MemoryUnavailable("MemoryCore request failed: " + str(error)) from error

    def search(self, query, isolation):
        data = self.post("/v3/conversation/search", dict(isolation, query=query, limit=10))
        if not isinstance(data, dict) or not isinstance(data.get("messages"), list):
            raise MemoryUnavailable("unexpected MemoryCore search response")
        return data["messages"]


def recall(client, corpus, query, workspace, projects, previous):
    by_id = {r["id"]: r for r in corpus["records"]}
    found = {}
    try:
        for project in projects:
            for hit in client.search(query, scope(workspace, project)):
                marker = SOURCE.search(hit.get("content", ""))
                if not marker or marker.group(1) not in by_id:
                    raise MemoryUnavailable("memory has no known source")
                record = by_id[marker.group(1)]
                if record["workspace"] != workspace or record["project"] != project:
                    raise MemoryUnavailable("memory source is outside requested scope")
                found[record["id"]] = dict(record, score=hit.get("score", 0))
        # Scores from different project searches need not be calibrated. This is
        # an explicit experimental merge, not a production ranking contract.
        items = sorted(found.values(), key=lambda r: (r["score"], r["date"]), reverse=True)[:3]
        return {"available": True, "stale": False, "items": items}
    except MemoryUnavailable as error:
        allowed = {r["id"] for r in corpus["records"]
                   if r["workspace"] == workspace and r["project"] in projects}
        cached = [r for r in previous if r.get("id") in allowed]
        return {"available": False, "stale": True, "items": copy.deepcopy(cached), "error": str(error)}


def build_brief(corpus, workspace, context):
    projects = {}
    for item in corpus["items"]:
        if item["workspace"] != workspace or item["closed"]:
            continue
        projects.setdefault(item["project"], []).append(copy.deepcopy(item))
    return {"version": 1, "workspace": workspace, "projects": [
        {"project": name, "items": items} for name, items in sorted(projects.items())
    ], "context": copy.deepcopy(context),
        "state_source": "fictional current-work snapshot; memory cannot close or reclassify items"}


def score(query, ids):
    expected = set(query["expected"])
    hits = expected & set(ids)
    return {"ids": ids, "recall_at_3": len(hits) / len(expected) if expected else None,
            "pass": expected.issubset(ids) if expected else not ids,
            "superseded_also_returned": sorted(set(query.get("superseded", [])) & set(ids)),
            "current_decision_first": (bool(ids) and ids[0] in expected) if "superseded" in query else None}


def start_gateway(source, runtime):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    token = secrets.token_urlsafe(32)
    config = {"server": {"host": "127.0.0.1", "port": port, "apiKey": token},
              "data": {"baseDir": str(runtime / "data")},
              "llm": {"baseUrl": "http://127.0.0.1:9/v1", "apiKey": "", "model": "disabled"},
              "memory": {"capture": {"enabled": True}, "extraction": {"enabled": False},
                         "llm": {"enabled": False}, "embedding": {"enabled": False},
                         "pipeline": {"enableWarmup": False, "everyNConversations": 1000000,
                                      "l1IdleTimeoutSeconds": 86400,
                                      "l2DelayAfterL1Seconds": 86400,
                                      "l2MinIntervalSeconds": 86400,
                                      "l2MaxIntervalSeconds": 86400}},
              "skill": {"enabled": False}}
    config_path = runtime / "gateway.json"
    config_path.write_text(json.dumps(config))
    config_path.chmod(0o600)
    home = runtime / "home"
    home.mkdir()
    env = {"PATH": os.environ["PATH"], "HOME": str(home), "TMPDIR": str(runtime),
           "TDAI_GATEWAY_CONFIG": str(config_path), "NODE_ENV": "production",
           "TDAI_API_TRACE_ENABLED": "false"}
    log = (runtime / "gateway.log").open("w")
    child = subprocess.Popen(["node", "--import", "tsx", "src/gateway/server.ts"],
                             cwd=source / "MemoryCore", env=env, stdout=log, stderr=subprocess.STDOUT)
    client = MemoryCore("http://127.0.0.1:" + str(port), token)
    deadline = time.monotonic() + 30
    try:
        while time.monotonic() < deadline:
            if child.poll() is not None:
                raise MemoryUnavailable("gateway exited; see isolated gateway.log")
            try:
                with client.opener.open(client.endpoint + "/health", timeout=1) as response:
                    health = json.load(response)
                return child, log, client, health
            except (URLError, OSError):
                time.sleep(0.1)
        raise MemoryUnavailable("gateway startup exceeded 30 seconds")
    except BaseException:
        stop_gateway(child)
        log.close()
        raise


def stop_gateway(child):
    if child.poll() is None:
        child.terminate()
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()


def run(source, output, corpus):
    output.mkdir(parents=True, exist_ok=False)
    report = {"version": 1, "upstream_commit": UPSTREAM_COMMIT,
              "environment": {"node": subprocess.check_output(["node", "--version"], text=True).strip(),
                              "system": platform.system(), "architecture": platform.machine()},
              "dependency_lock_sha256": hashlib.sha256((source / "MemoryCore/package-lock.json").read_bytes()).hexdigest(),
              "mode": "local retrieval; automatic extraction disabled",
              "not_measured": ["LLM extraction accuracy", "automatic conflict resolution", "embedding recall", "production workload performance"],
              "baseline": "unique keyword overlap over original handoffs, scoped before ranking; newest breaks ties",
              "corpus_sha256": hashlib.sha256(json.dumps(corpus, sort_keys=True).encode()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="sous-memory-runtime-") as temporary:
        runtime = Path(temporary)
        child = log = None
        try:
            child, log, client, health = start_gateway(source, runtime)
            report["health"] = health
            if not health.get("stores", {}).get("ftsAvailable"):
                raise MemoryUnavailable("SQLite full-text search is unavailable; empty results would be misleading")
            counts = []
            for record in corpus["records"]:
                payload = dict(scope(record["workspace"], record["project"]), session_id=record["session"], messages=[{
                    "role": "user", "content": "[sous-source:" + record["id"] + "] " + record["text"],
                    "timestamp": record["date"]}])
                counts.append(client.post("/v3/conversation/add", payload))
            report["ingestion"] = counts
            results = []
            for query in corpus["queries"]:
                baseline = baseline_search(corpus, query["query"], query["workspace"], query["projects"])
                started = time.monotonic()
                result = recall(client, corpus, query["query"], query["workspace"], query["projects"], [])
                measured = score(query, [r["id"] for r in result["items"]])
                measured.update(available=result["available"], elapsed_ms=round((time.monotonic() - started) * 1000, 2))
                if not result["available"]:
                    measured.update(error=result["error"], **{"pass": False})
                results.append({"id": query["id"], "query": query["query"], "expected": query["expected"],
                                "baseline": score(query, [r["id"] for r in baseline]), "memory_core": measured})
            report["queries"] = results
            # L2 write only updates existing summaries in this revision. With
            # extraction deferred, execute its real scope function separately.
            scope_probe = subprocess.run([
                "node", "--import", "tsx", str(Path(__file__).with_name("probe-scope.mjs")), str(source)
            ], cwd=source / "MemoryCore", env={"PATH": os.environ["PATH"], "HOME": str(runtime / "home")},
                capture_output=True, text=True, timeout=10, check=True)
            report["summary_isolation"] = json.loads(scope_probe.stdout)
            record = corpus["records"][0]
            isolation = dict(scope(record["workspace"], record["project"]), session_id=record["session"])
            before = client.post("/v3/conversation/count", isolation)
            replay = client.post("/v3/conversation/add", dict(isolation, messages=[{
                "role": "user", "content": "[sous-source:" + record["id"] + "] " + record["text"],
                "timestamp": record["date"]}]))
            after = client.post("/v3/conversation/count", isolation)
            report["ingestion_retry"] = {"before": before, "after": after, "replay": replay}
            cached = recall(client, corpus, "idempotency key", "work", ["acme/api", "acme/site"], [])
            write_json(output / "work-brief.json", build_brief(corpus, "work", cached))
            with client.opener.open(client.endpoint + "/health", timeout=2) as response:
                report["health_after"] = json.load(response)
            if report["health_after"]["services"]["pipelineWorker"]["tasksConsumed"] != 0:
                raise MemoryUnavailable("unexpected extraction task in local-only experiment")
            stop_gateway(child)
            degraded = recall(client, corpus, "idempotency key", "work", ["acme/api", "acme/site"], cached["items"])
            report["offline_fallback"] = {"available": degraded["available"], "stale": degraded["stale"],
                                          "preserved_ids": [r["id"] for r in degraded["items"]]}
            report["summary"] = {
                "queries": len(results),
                "baseline_passes": sum(q["baseline"]["pass"] for q in results),
                "memory_core_passes": sum(q["memory_core"]["pass"] for q in results),
                "extraction_tasks": report["health_after"]["services"]["pipelineWorker"]["tasksConsumed"],
            }
            report["completed"] = True
        except (MemoryUnavailable, KeyError, ValueError, subprocess.SubprocessError) as error:
            report.update(completed=False, error=str(error))
        finally:
            if child:
                stop_gateway(child)
            if log:
                log.close()
            if (runtime / "gateway.log").exists():
                shutil.copyfile(runtime / "gateway.log", output / "gateway.log")
    write_json(output / "results.json", report)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True, help="prepared pinned upstream checkout")
    parser.add_argument("--output", type=Path, required=True, help="new output directory")
    args = parser.parse_args()
    report = run(args.source.resolve(), args.output.resolve(), load_corpus(Path(__file__).with_name("corpus.json")))
    print(json.dumps({"completed": report["completed"], "results": str(args.output / "results.json")}, indent=2))
    return 0 if report["completed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
