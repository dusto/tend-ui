import { test } from "node:test";
import assert from "node:assert/strict";

import { relativeTime } from "../format.js";

// Fixed reference instant so the buckets are deterministic.
const now = 1_700_000_000_000;

test("relativeTime renders the short buckets", () => {
  assert.equal(relativeTime(now - 2_000, now), "now");
  assert.equal(relativeTime(now - 12_000, now), "12s ago");
  assert.equal(relativeTime(now - 5 * 60_000, now), "5m ago");
  assert.equal(relativeTime(now - 3 * 3_600_000, now), "3h ago");
  assert.equal(relativeTime(now - 2 * 86_400_000, now), "2d ago");
});

test("relativeTime clamps a future instant to now", () => {
  assert.equal(relativeTime(now + 5_000, now), "now");
});
