// Small dependency-free helpers for the tend-ui frontend. Pure functions, unit
// tested with Node's built-in runner (web/assets/js/_tests/, run via `npm test`).

/**
 * relativeTime renders how long ago `then` was compared to `now` as a short
 * label — "now", "12s ago", "5m ago", "3h ago", "2d ago". Both are epoch ms; a
 * future `then` clamps to "now".
 * @param {number} then epoch milliseconds
 * @param {number} [now] epoch milliseconds (defaults to Date.now())
 * @returns {string}
 */
export function relativeTime(then, now = Date.now()) {
  const sec = Math.max(0, Math.floor((now - then) / 1000));
  if (sec < 5) return "now";
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}
