"""Adapter checks use fixtures and fake HTTP responses, never personal state."""
import copy
import json
from pathlib import Path
import unittest
from unittest.mock import patch

from experiment import (MemoryCore, MemoryUnavailable, baseline_search,
                        build_brief, load_corpus, recall, scope)


class MemoryExperimentTests(unittest.TestCase):
    def setUp(self):
        self.corpus = load_corpus(Path(__file__).with_name("corpus.json"))

    def test_unavailable_engine_retains_previous_context_and_marks_it_stale(self):
        previous = [{"id": "api-2", "text": "Previously retrieved context"}]
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "search", side_effect=MemoryUnavailable("offline")):
            result = recall(client, self.corpus, "pagination", "work", ["acme/api"], previous)
        self.assertFalse(result["available"])
        self.assertTrue(result["stale"])
        self.assertEqual(result["items"], previous)
        self.assertIn("offline", result["error"])

    def test_fallback_cannot_reuse_another_workspaces_cache(self):
        previous = [next(r for r in self.corpus["records"] if r["id"] == "library-1")]
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "search", side_effect=MemoryUnavailable("offline")):
            result = recall(client, self.corpus, "pagination", "work", ["acme/api"], previous)
        self.assertFalse(result["available"])
        self.assertEqual(result["items"], [])

    def test_partial_search_failure_preserves_cache_as_one_snapshot(self):
        previous = [{"id": "site-2", "text": "Cached project context"}]
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "search", side_effect=[
            [{"content": "[sous-source:api-2] cursor pagination"}], MemoryUnavailable("second project offline")
        ]):
            result = recall(client, self.corpus, "pagination", "work", ["acme/api", "acme/site"], previous)
        self.assertFalse(result["available"])
        self.assertEqual(result["items"], previous)

    def test_successful_empty_search_is_distinct_from_unavailable(self):
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "search", return_value=[]):
            result = recall(client, self.corpus, "unicorn", "work", ["acme/api"], [])
        self.assertTrue(result["available"])
        self.assertFalse(result["stale"])
        self.assertEqual(result["items"], [])

    def test_search_reads_the_actual_gateway_response(self):
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "post", return_value={"messages": [{"content": "evidence"}]}):
            self.assertEqual(client.search("evidence", scope("work", "acme/api")), [{"content": "evidence"}])

    def test_malformed_search_is_unavailable_not_empty(self):
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "post", return_value={"unexpected": []}):
            with self.assertRaises(MemoryUnavailable):
                client.search("evidence", scope("work", "acme/api"))

    def test_project_scope_does_not_depend_on_session_or_harness(self):
        first = scope("work", "acme/api")
        self.assertEqual(first, scope("work", "acme/api"))
        self.assertNotEqual(first["agent_id"], scope("work", "acme/site")["agent_id"])
        self.assertNotEqual(first["team_id"], scope("personal", "acme/api")["team_id"])

    def test_baseline_filters_workspace_before_matching(self):
        results = baseline_search(self.corpus, "violet-canary", "work", ["acme/api", "acme/site"])
        self.assertEqual(results, [])
        results = baseline_search(self.corpus, "violet-canary", "personal", ["home/library"])
        self.assertEqual([item["id"] for item in results], ["library-1"])

    def test_adapter_rejects_a_source_outside_requested_scope(self):
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "search", return_value=[{"content": "[sous-source:library-1] violet-canary"}]):
            result = recall(client, self.corpus, "violet", "work", ["acme/api"], [])
        self.assertFalse(result["available"])
        self.assertIn("scope", result["error"])

    def test_recall_requires_a_traceable_source(self):
        client = MemoryCore("http://127.0.0.1:1", "fixture")
        with patch.object(client, "search", return_value=[{"content": "A claim with no source"}]):
            result = recall(client, self.corpus, "claim", "work", ["acme/api"], [])
        self.assertFalse(result["available"])
        self.assertIn("source", result["error"])

    def test_brief_uses_current_work_state_and_preserves_input(self):
        before = copy.deepcopy(self.corpus)
        memories = {"available": True, "stale": False, "items": [{"id": "api-4", "text": "The release is done."}]}
        brief = build_brief(self.corpus, "work", memories)
        self.assertEqual([p["project"] for p in brief["projects"]], ["acme/api", "acme/site"])
        tasks = {task["id"]: task for p in brief["projects"] for task in p["items"]}
        self.assertEqual(tasks["api-smoke"]["section"], "on_you")
        self.assertEqual(tasks["site-design"]["section"], "on_others")
        self.assertNotIn("api-old-review", tasks)
        self.assertNotIn("library-import", tasks)
        self.assertEqual(self.corpus, before)

    def test_newer_fixture_format_is_refused(self):
        with patch("pathlib.Path.read_text", return_value=json.dumps({"version": 999})):
            with self.assertRaisesRegex(ValueError, "version"):
                load_corpus(Path("fixture.json"))


if __name__ == "__main__":
    unittest.main()
