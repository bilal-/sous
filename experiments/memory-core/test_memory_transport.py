"""HTTP response failures must take the same scoped fallback as offline errors."""
import copy
from http.client import IncompleteRead
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch

import experiment
from experiment import MemoryCore, load_corpus, recall


class TruncatedResponse(io.BytesIO):
    def read(self, *args, **kwargs):
        raise IncompleteRead(b'{"code":0,"data":', 32)


class MemoryTransportTests(unittest.TestCase):
    def setUp(self):
        self.corpus = load_corpus(Path(experiment.__file__).with_name("corpus.json"))
        self.client = MemoryCore("http://127.0.0.1:1", "fixture")
        self.previous = [
            {"id": "api-2", "text": "Saved context", "source": {"note": "original"}},
            {"id": "library-1", "text": "Personal canary"},
        ]

    def assert_stale_and_scoped(self, result):
        self.assertFalse(result["available"])
        self.assertTrue(result["stale"])
        self.assertEqual(result["items"], self.previous[:1])
        self.assertIsNot(result["items"][0], self.previous[0])
        self.assertIsNot(result["items"][0]["source"], self.previous[0]["source"])
        self.assertTrue(result["error"])

    def test_incomplete_http_response_keeps_scoped_context(self):
        before = copy.deepcopy(self.previous)
        with patch.object(self.client.opener, "open", return_value=TruncatedResponse()):
            result = recall(self.client, self.corpus, "retry", "work", ["acme/api"], self.previous)
        self.assert_stale_and_scoped(result)
        self.assertEqual(self.previous, before)

    def test_incomplete_second_response_preserves_snapshot(self):
        valid = io.BytesIO(json.dumps({"code": 0, "data": {"messages": [
            {"content": "[sous-source:api-1] Fresh result", "score": 1}
        ]}}).encode())
        before = copy.deepcopy(self.previous)
        with patch.object(self.client.opener, "open", side_effect=[valid, TruncatedResponse()]):
            result = recall(self.client, self.corpus, "retry", "work", ["acme/api", "acme/site"], self.previous)
        self.assert_stale_and_scoped(result)
        self.assertEqual(self.previous, before)


if __name__ == "__main__":
    unittest.main()
