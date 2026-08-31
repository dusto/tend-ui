// Auto-scroll the timeline as new events stream in. The scroll container is
// .log-wrap (not #timeline, which htmx's `scroll:bottom` targets — it isn't the
// scrolling element, so that modifier is a no-op). We watch the log for DOM
// changes and keep it pinned to the bottom, but only while the user is already
// at the bottom: if they scroll up to read history, we stop following so we
// don't yank them back down. Dependency-free ES module (see AGENTS.md).

// nearBottom reports whether a scroll position is within `threshold` px of the
// bottom — the test for "the user is following the live tail". Pure, so it is
// unit-tested without a DOM.
export function nearBottom(scrollTop, scrollHeight, clientHeight, threshold = 80) {
	return scrollHeight - scrollTop - clientHeight <= threshold;
}

// initAutoScroll wires the timeline log to follow new content. Idempotent-ish:
// it no-ops when the log is absent. Returns a teardown for tests.
export function initAutoScroll(doc = document) {
	const wrap = doc.querySelector(".log-wrap");
	if (!wrap) {
		return () => {};
	}
	let following = true;

	const onScroll = () => {
		following = nearBottom(wrap.scrollTop, wrap.scrollHeight, wrap.clientHeight);
	};
	wrap.addEventListener("scroll", onScroll, { passive: true });

	const stick = () => {
		if (following) {
			wrap.scrollTop = wrap.scrollHeight;
		}
	};
	// New timeline items (SSE beforeend swaps), out-of-band card updates, and the
	// whole panel being replaced on a session switch all mutate the log subtree.
	const obs = new MutationObserver(stick);
	obs.observe(wrap, { childList: true, subtree: true, characterData: true });

	stick(); // start pinned to the newest content
	return () => {
		obs.disconnect();
		wrap.removeEventListener("scroll", onScroll);
	};
}

if (typeof document !== "undefined") {
	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", () => initAutoScroll());
	} else {
		initAutoScroll();
	}
}
