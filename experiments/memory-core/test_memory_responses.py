"""MemoryCore response-boundary tests; standard library and fake HTTP only."""
import copy
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch

import experiment
from experiment import MemoryCore, load_corpus, recall


class MemoryResponseTests(unittest.TestCase):
    def setUp(self):
        self.corpus = load_corpus(Path(experiment.__file__).with_name("corpus.json"))
        self.client = MemoryCore("http://127.0.0.1:1", "fixture")
        self.previous = [{"id": "api-2", "text": "Saved context", "source": {"note": "original"}}]

    @staticmethod
    def response(payload):
        return io.BytesIO(json.dumps(payload).encode())

    def recall_payload(self, payload, previous=None):
        if previous is None:
            previous = self.previous
        with patch.object(self.client.opener, "open", return_value=self.response(payload)):
            return recall(self.client, self.corpus, "retry", "work", ["acme/api"], previous)

    def assert_stale(self, result):
        self.assertFalse(result["available"])
        self.assertTrue(result["stale"])
        self.assertEqual(result["items"], self.previous)
        self.assertTrue(result["error"])
        self.assertIsNot(result["items"], self.previous)
        self.assertIsNot(result["items"][0], self.previous[0])
        self.assertIsNot(result["items"][0]["source"], self.previous[0]["source"])

    def test_invalid_response_shapes_keep_previous_context(self):
        payloads = [None, [], "changed envelope", {"code": 0},
                    {"code": 1, "data": {}}, {"code": 0, "data": None},
                    {"code": 0, "data": []}, {"code": 0, "data": {}},
                    {"code": 0, "data": {"messages": None}},
                    {"code": 0, "data": {"messages": {}}}]
        for payload in payloads:
            with self.subTest(payload=payload):
                self.assert_stale(self.recall_payload(payload))

    def test_malformed_hits_and_content_keep_previous_context(self):
        malformed = [None, [], 7, "not a hit", {}, {"content": None},
                     {"content": []}, {"content": {}}, {"content": 7},
                     {"content": False}]
        valid = {"content": "[sous-source:api-1] original", "score": 1}
        for hit in malformed:
            with self.subTest(hit=hit):
                self.assert_stale(self.recall_payload({
                    "code": 0, "data": {"messages": [valid, hit]}}))

    def test_invalid_scores_keep_previous_context(self):
        invalid = [None, "2", {}, [], True, False,
                   float("nan"), float("inf"), float("-inf")]
        valid = {"content": "[sous-source:api-1] original", "score": 1}
        for score in invalid:
            with self.subTest(score=score):
                self.assert_stale(self.recall_payload({
                    "code": 0, "data": {"messages": [valid,
                        {"content": "[sous-source:api-3] candidate", "score": score}]}}))

    def test_valid_scores_and_missing_score_keep_ranking_contract(self):
        for highest_score in (2.5, 10**1000):
            with self.subTest(highest_score=highest_score):
                result = self.recall_payload({"code": 0, "data": {"messages": [
                    {"content": "[sous-source:api-1] default score"},
                    {"content": "[sous-source:api-2] highest score", "score": highest_score},
                    {"content": "[sous-source:api-3] integer score", "score": 2},
                    {"content": "[sous-source:api-4] negative score", "score": -1.5},
                ]}})
                self.assertTrue(result["available"])
                self.assertFalse(result["stale"])
                self.assertEqual([r["id"] for r in result["items"]], ["api-2", "api-3", "api-1"])
                self.assertEqual(result["items"][-1]["score"], 0)

    def test_malformed_second_project_keeps_one_previous_snapshot(self):
        responses = [
            self.response({"code": 0, "data": {"messages": [
                {"content": "[sous-source:api-1] fresh", "score": 1}]}}),
            self.response({"code": 0, "data": {"messages": [None]}}),
        ]
        with patch.object(self.client.opener, "open", side_effect=responses):
            result = recall(self.client, self.corpus, "retry", "work",
                            ["acme/api", "acme/site"], self.previous)
        self.assert_stale(result)

    def test_malformed_response_fallback_stays_within_requested_scope(self):
        previous = copy.deepcopy(self.previous) + [
            {"id": "site-2", "text": "Another work project"},
            {"id": "library-1", "text": "Personal canary"},
            {"id": "unknown-source", "text": "Untraceable context"},
        ]
        before = copy.deepcopy(previous)
        result = self.recall_payload({"code": 0, "data": {"messages": [None]}}, previous)
        self.assert_stale(result)
        self.assertEqual(previous, before)


if __name__ == "__main__":
    unittest.main()
