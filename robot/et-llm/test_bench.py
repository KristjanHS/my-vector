# Offline tests for bench.py fit() + resume key: python3 -I -m unittest discover -s robot/et-llm -p 'test_*.py'
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import bench  # noqa: E402

ROW = {
    "llm_mib": 5547,
    "gpu_total_mib": 8192,
    "size_mib": 5000,
    "size_vram_mib": 5000,
}


class Fit(unittest.TestCase):
    def setUp(self):
        bench.use("en")

    def test_en_boundary(self):
        self.assertEqual(bench.fit(ROW), (0, "yes"))
        self.assertEqual(bench.fit(ROW | {"llm_mib": 5548}), (-1, "no"))

    def test_spill_on_raw_bytes(self):
        # equal MiB, but raw bytes show a partial CPU offload
        row = ROW | {"size_b": 5000 << 20, "size_vram_b": (5000 << 20) - 1}
        self.assertEqual(bench.fit(row), (None, "spill"))

    def test_missing_ps_unknown(self):
        self.assertEqual(
            bench.fit(ROW | {"size_mib": None, "size_vram_mib": None}),
            (None, "unknown"),
        )

    def test_et_defers_to_stack(self):
        bench.use("et")
        self.assertEqual(bench.fit(ROW), (None, "stack.py"))


class DoneKey(unittest.TestCase):
    def test_only_current_ollama_and_req(self):
        rows = [
            {"model": "m", "qid": 1, "ollama": "0.40.14", "req": bench.REQ_TAG},
            {"model": "m", "qid": 2, "ollama": "0.40.13", "req": bench.REQ_TAG},
            {"model": "m", "qid": 3, "ollama": "0.40.14", "req": "re-other"},
            {"model": "m", "qid": 4},
        ]
        done = bench.done_keys(rows)
        live = {q for q in (1, 2, 3, 4) if ("m", q, "0.40.14", bench.REQ_TAG) in done}
        self.assertEqual(live, {1})


if __name__ == "__main__":
    unittest.main()
