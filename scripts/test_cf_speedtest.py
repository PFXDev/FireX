"""Regression tests for the observed speed endpoint size restriction."""
import importlib.util
import unittest
import urllib.parse
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("cf_speedtest", Path(__file__).with_name("cf-speedtest.py"))
cf = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cf)
MIB = 1048576


class DownloadTests(unittest.TestCase):
    def test_default_16_mib_respects_endpoint_limit_and_counts_total_time(self):
        requests = []

        def endpoint(args, timeout):
            size = int(urllib.parse.parse_qs(urllib.parse.urlsplit(args[-1]).query)["bytes"][0])
            requests.append((size, timeout))
            if size > 10 * MIB:
                return {"status": 403, "bytes": 1, "seconds": 0.1, "exit": 0}, ""
            return {"status": 200, "bytes": size, "seconds": 2, "exit": 0}, ""

        with patch.object(cf, "curl_metrics", side_effect=endpoint), \
                patch.object(cf.time, "monotonic", side_effect=[100, 100, 102, 104]):
            result = cf.download_via_proxy("http://127.0.0.1:7890", 16 * MIB, 25)
        self.assertEqual(requests, [(8 * MIB, 25), (8 * MIB, 23)])
        self.assertTrue(result["download_ok"])
        self.assertEqual(result["download_bytes"], 16 * MIB)
        self.assertEqual(result["download_mib_s"], 4)

    def test_last_request_matches_remaining_bytes(self):
        sizes = []

        def endpoint(args, timeout):
            size = int(urllib.parse.parse_qs(urllib.parse.urlsplit(args[-1]).query)["bytes"][0])
            sizes.append(size)
            return {"status": 200, "bytes": size, "seconds": 1, "exit": 0}, ""

        with patch.object(cf, "curl_metrics", side_effect=endpoint), \
                patch.object(cf.time, "monotonic", side_effect=[0, 0, 1, 2, 3]):
            result = cf.download_via_proxy("http://127.0.0.1:7890", 17 * MIB + 7, 25)
        self.assertEqual(sizes, [8 * MIB, 8 * MIB, MIB + 7])
        self.assertEqual(result["download_bytes"], 17 * MIB + 7)
        self.assertTrue(result["download_ok"])

    def test_rejected_response_is_not_a_download_speed(self):
        with patch.object(cf, "curl_metrics", return_value=({"status": 403, "bytes": 1, "exit": 0}, "")):
            result = cf.download_via_proxy("http://127.0.0.1:7890", 16 * MIB, 25)
        self.assertFalse(result["download_ok"])
        self.assertIsNone(result["download_mib_s"])
        self.assertIn("HTTP 403", result["error"])
        self.assertEqual(len(result["download_requests"]), 1)

    def test_timeout_during_second_request_preserves_partial_bytes(self):
        responses = [({"status": 200, "bytes": 8 * MIB, "exit": 0}, ""),
                     ({"status": 200, "bytes": MIB, "exit": 28}, "")]
        with patch.object(cf, "curl_metrics", side_effect=responses), \
                patch.object(cf.time, "monotonic", side_effect=[0, 0, 10, 25]):
            result = cf.download_via_proxy("http://127.0.0.1:7890", 16 * MIB, 25)
        self.assertFalse(result["download_ok"])
        self.assertIsNone(result["download_mib_s"])
        self.assertEqual(result["download_bytes"], 9 * MIB)
        self.assertIn("下载超时", result["error"])

    def test_short_http_200_body_is_not_a_success(self):
        with patch.object(cf, "curl_metrics", return_value=({"status": 200, "bytes": 100, "exit": 0}, "")):
            result = cf.download_via_proxy("http://127.0.0.1:7890", 16 * MIB, 25)
        self.assertFalse(result["download_ok"])
        self.assertIn("响应不完整", result["error"])


class QualificationTests(unittest.TestCase):
    def evaluate(self, latency=190, speed=15, success=3):
        summary = {"ip": "104.17.26.239", "control": False, "success": success, "rounds": 3,
                   "median_latency_ms": latency, "median_download_mib_s": speed}
        return cf.qualify(summary, 190, 15)

    def test_thresholds_include_equality(self):
        self.assertTrue(self.evaluate()["eligible"])

    def test_fast_download_cannot_override_high_latency(self):
        self.assertFalse(self.evaluate(latency=190.1, speed=100)["eligible"])

    def test_low_latency_cannot_override_slow_download(self):
        self.assertFalse(self.evaluate(latency=100, speed=14.999)["eligible"])

    def test_partial_success_and_missing_values_are_not_eligible(self):
        self.assertFalse(self.evaluate(latency=100, speed=100, success=2)["eligible"])
        self.assertFalse(self.evaluate(latency=None, speed=100)["eligible"])
        self.assertFalse(self.evaluate(latency=100, speed=None)["eligible"])


class RecommendationTests(unittest.TestCase):
    def row(self, ip, latency, speed, success=3):
        return cf.qualify({"ip": ip, "success": success, "rounds": 3,
                           "median_latency_ms": latency, "median_download_mib_s": speed}, 190, 15)

    def test_qualified_candidate_wins_over_faster_unqualified_candidate(self):
        result = cf.choose_recommendation([self.row("a", 191, 100), self.row("b", 190, 15)])
        self.assertEqual(result["ip"], "b")
        self.assertFalse(result["fallback"])

    def test_no_qualified_candidate_selects_highest_speed(self):
        result = cf.choose_recommendation([self.row("a", 150, 3), self.row("b", 249, 9.249)])
        self.assertEqual(result["ip"], "b")
        self.assertTrue(result["fallback"])
        self.assertFalse(result["eligible"])

    def test_speed_tie_prefers_lower_latency(self):
        result = cf.choose_recommendation([self.row("a", 249, 9), self.row("b", 200, 9)])
        self.assertEqual(result["ip"], "b")

    def test_partial_or_failed_download_is_not_a_fallback(self):
        self.assertIsNone(cf.choose_recommendation([self.row("a", 100, 100, success=2)]))
        result = cf.choose_recommendation([self.row("a", 100, 100, success=2), self.row("b", 200, 9)])
        self.assertEqual(result["ip"], "b")


if __name__ == "__main__":
    unittest.main()
