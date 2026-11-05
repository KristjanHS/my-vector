# Offline tests for stack.py verdict() + ctx_estimate(): python3 -I -m unittest discover -s robot/et-llm -p 'test_*.py'
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import bench  # noqa: E402
import stack  # noqa: E402

# need = (peak - base) + (worst base - Kokoro) + margin; zero headroom at peak - base = total - worst + Kokoro - margin
EDGE = 8192 - bench.WORST_BASE_MIB + 700 - bench.MARGIN_MIB
ROW = {
    "size_b": 5,
    "size_vram_b": 5,
    "gpu_base_mib": 2000,
    "gpu_total_mib": 8192,
    "kokoro_mib": 700,
}


class Verdict(unittest.TestCase):
    def test_boundary_subtracts_kokoro(self):
        self.assertEqual(stack.verdict(ROW | {"peak_mib": 2000 + EDGE}), (0, "yes"))
        self.assertEqual(stack.verdict(ROW | {"peak_mib": 2001 + EDGE}), (-1, "no"))

    def test_spill(self):
        self.assertEqual(
            stack.verdict(ROW | {"peak_mib": 2000, "size_vram_b": 4}), (None, "spill")
        )


class CtxEstimate(unittest.TestCase):
    def row(self, quant, ctx, headroom):
        return {"quant": quant, "num_ctx": ctx, "headroom_mib": headroom}

    def test_applies_q4km_saving(self):
        rows = [
            self.row("i1-Q4_K_M", None, -700),
            self.row("i1-Q4_K_M", 2048, -400),
            self.row("i1-Q4_K_S", None, -500),
        ]
        self.assertEqual(stack.ctx_estimate(rows, "i1-Q4_K_S", 2048), -200)

    def test_unmeasured_is_none(self):
        rows = [self.row("i1-Q4_K_M", None, -700), self.row("i1-Q4_K_S", None, -500)]
        self.assertIsNone(stack.ctx_estimate(rows, "i1-Q4_K_S", 2048))


if __name__ == "__main__":
    unittest.main()
