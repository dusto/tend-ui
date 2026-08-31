import { test } from "node:test";
import assert from "node:assert/strict";

import { nearBottom } from "../autoscroll.js";

test("nearBottom is true at the very bottom", () => {
	// scrollTop == scrollHeight - clientHeight -> exactly at bottom.
	assert.equal(nearBottom(900, 1000, 100), true);
});

test("nearBottom stays true within the threshold", () => {
	// 60px from the bottom, default threshold 80 -> still following.
	assert.equal(nearBottom(840, 1000, 100), true);
});

test("nearBottom is false when scrolled up past the threshold", () => {
	// 300px from the bottom -> the user is reading history.
	assert.equal(nearBottom(600, 1000, 100), false);
});

test("nearBottom honors a custom threshold", () => {
	assert.equal(nearBottom(600, 1000, 100, 400), true);
	assert.equal(nearBottom(600, 1000, 100, 100), false);
});
