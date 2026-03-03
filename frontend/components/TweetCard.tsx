"use client";

import { useState, useEffect } from "react";
import type { Tweet, EngagementSnapshot } from "@/lib/types";

// ── Palette ─────────────────────────────────────────────────────────────────
const AVATAR_COLORS = [
  "#1d9bf0",
  "#00ba7c",
  "#f4212e",
  "#ff7708",
  "#7856ff",
  "#ffd400",
];

function avatarColor(username: string): string {
  let h = 0;
  for (const c of username) h = ((h * 31) + c.charCodeAt(0)) | 0;
  return AVATAR_COLORS[Math.abs(h) % AVATAR_COLORS.length];
}

function firstChar(s: string): string {
  return Array.from(s)[0] ?? "";
}

function initials(displayName: string | null, username: string): string {
  const name = (displayName || username).trim();
  // Only use words that start with a Unicode letter — skips emoji, punctuation
  const words = name.split(/\s+/).filter((w) => /^\p{L}/u.test(w));
  // Use Array.from to avoid splitting surrogate pairs (e.g. 𝕳, 𝔸)
  if (words.length >= 2)
    return (firstChar(words[0]) + firstChar(words[words.length - 1])).toUpperCase();
  if (words.length === 1)
    return Array.from(words[0]).slice(0, 2).join("").toUpperCase();
  return username.slice(0, 2).toUpperCase();
}

// Strip the "RT @username: " prefix so we show just the original text
function stripRtPrefix(text: string, originalUsername: string): string {
  const prefix = `RT @${originalUsername}: `;
  return text.startsWith(prefix) ? text.slice(prefix.length) : text;
}

function formatAbsolute(iso: string): string {
  return new Date(iso)
    .toLocaleString("en-US", {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "numeric",
      minute: "2-digit",
      timeZoneName: "short",
    });
}

// useEffect so timezone resolves on the client (server runs UTC, client has local tz)
function AbsoluteTime({ iso, className }: { iso: string; className?: string }) {
  const [label, setLabel] = useState<string | null>(null);

  useEffect(() => {
    setLabel(formatAbsolute(iso));
  }, [iso]);

  // Stable server fallback — pin to UTC so server & client agree before useEffect kicks in
  const fallback = new Date(iso).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZone: "UTC",
  });

  return <span className={className}>{label ?? fallback}</span>;
}

// ── Sub-components ───────────────────────────────────────────────────────────
function Avatar({
  username,
  displayName,
  size = 40,
}: {
  username: string;
  displayName: string | null;
  size?: number;
}) {
  return (
    <div
      className="rounded-full flex items-center justify-center flex-shrink-0 font-bold text-white select-none"
      style={{
        width: size,
        height: size,
        minWidth: size,
        backgroundColor: avatarColor(username),
        fontSize: Math.round(size * 0.36),
      }}
    >
      {initials(displayName, username)}
    </div>
  );
}

function TweetText({ text }: { text: string }) {
  const parts = text.split(/(https?:\/\/\S+|@\w+)/g);
  return (
    <>
      {parts.map((part, i) => {
        if (/^https?:\/\//.test(part)) {
          return (
            <a
              key={i}
              href={part}
              target="_blank"
              rel="noopener noreferrer"
              className="text-[#1d9bf0] hover:underline break-all"
              onClick={(e) => e.stopPropagation()}
            >
              {part}
            </a>
          );
        }
        if (/^@\w+/.test(part)) {
          return (
            <span key={i} className="text-[#1d9bf0]">
              {part}
            </span>
          );
        }
        return <span key={i}>{part}</span>;
      })}
    </>
  );
}

function formatCount(n: number | null): string {
  if (n == null || n === 0) return "0";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function EngagementRow({ tweet }: { tweet: Tweet }) {
  const hasViews = (tweet.views ?? 0) > 0;
  return (
    <div className="flex items-center gap-4 mt-3 text-[#71767b] text-xs flex-wrap">
      {/* Replies */}
      <span className="flex items-center gap-1">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
          <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
        </svg>
        {formatCount(tweet.replies)}
      </span>

      {/* Reposts */}
      <span className="flex items-center gap-1">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
          <path d="M4.5 3.88l4.432 4.14-1.364 1.46L5.5 7.55V16c0 1.1.896 2 2 2H13v2H7.5c-2.209 0-4-1.791-4-4V7.55L1.432 9.48.068 8.02 4.5 3.88zM16.5 6H11V4h5.5c2.209 0 4 1.791 4 4v8.45l2.068-1.93 1.364 1.46-4.432 4.14-4.432-4.14 1.364-1.46 2.068 1.93V8c0-1.104-.896-2-2-2z" />
        </svg>
        {formatCount(tweet.reposts)}
      </span>

      {/* Quotes */}
      <span className="flex items-center gap-1">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
          <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.401 6.231H2.748l7.73-8.835L1.254 2.25H8.08l4.258 5.63 5.906-5.63zm-1.161 17.52h1.833L7.084 4.126H5.117L17.083 19.77z" />
        </svg>
        {formatCount(tweet.quotes)}
      </span>

      {/* Likes */}
      <span className="flex items-center gap-1">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
          <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" />
        </svg>
        {formatCount(tweet.likes)}
      </span>

      {/* Bookmarks */}
      <span className="flex items-center gap-1">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
          <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" />
        </svg>
        {formatCount(tweet.bookmarks)}
      </span>

      {/* Views — only shown when available (own tweets) */}
      {hasViews && (
        <span className="flex items-center gap-1">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          {formatCount(tweet.views)}
        </span>
      )}
    </div>
  );
}

// Full label order (0m is baseline, not displayed as a row)
const LABEL_ORDER = ["0m", "15m", "30m", "45m", "60m"];
const DISPLAY_LABELS = ["0m", "15m", "30m", "45m", "60m"];

function formatDelta(n: number): string {
  if (n === 0) return "0";
  const abs = Math.abs(n);
  let formatted: string;
  if (abs >= 1_000_000) formatted = `${(abs / 1_000_000).toFixed(1)}M`;
  else if (abs >= 1_000) formatted = `${(abs / 1_000).toFixed(1)}K`;
  else formatted = String(abs);
  return n > 0 ? `+${formatted}` : `-${formatted}`;
}

function SnapshotTable({ snapshots }: { snapshots: EngagementSnapshot[] }) {
  // Build label -> snapshot map
  const snapMap = new Map<string, EngagementSnapshot>();
  for (const snap of snapshots) {
    snapMap.set(snap.label, snap);
  }

  // Only show rows where the snapshot has been captured
  const displayRows = DISPLAY_LABELS.filter((label) => snapMap.has(label));
  if (displayRows.length === 0) return null;

  return (
    <div
      className="mt-2 rounded-lg overflow-hidden text-[10px]"
      style={{ backgroundColor: "#16181c" }}
      onClick={(e) => e.stopPropagation()}
    >
      <table className="w-full">
        <thead>
          <tr className="text-[#71767b] border-b" style={{ borderColor: "#2f3336" }}>
            <th className="py-1.5 px-2 text-left font-medium"></th>
            <th className="py-1.5 px-2 text-right font-medium">Views</th>
            <th className="py-1.5 px-2 text-right font-medium">Likes</th>
            <th className="py-1.5 px-2 text-right font-medium">Reposts</th>
            <th className="py-1.5 px-2 text-right font-medium">Replies</th>
            <th className="py-1.5 px-2 text-right font-medium">Bookmarks</th>
          </tr>
        </thead>
        <tbody>
          {displayRows.map((label) => {
            const current = snapMap.get(label)!;
            const prevIdx = LABEL_ORDER.indexOf(label) - 1;
            const prevLabel = prevIdx >= 0 ? LABEL_ORDER[prevIdx] : null;
            const prev = prevLabel ? snapMap.get(prevLabel) : undefined;

            const cell = (metric: "views" | "likes" | "reposts" | "replies" | "bookmarks") => {
              if (label === "0m") return formatCount(current[metric] as number);
              if (!prev) return "\u2014";
              return formatDelta((current[metric] as number) - (prev[metric] as number));
            };

            return (
              <tr
                key={label}
                className="border-b last:border-b-0"
                style={{ borderColor: "#2f3336" }}
              >
                <td className="py-1.5 px-2 text-[#1d9bf0] font-semibold whitespace-nowrap">
                  @{label}
                  {label === "0m" && (
                    <AbsoluteTime iso={current.captured_at} className="ml-1 text-[#71767b] font-normal" />
                  )}
                </td>
                <td className="py-1.5 px-2 text-right text-[#e7e9ea]">{cell("views")}</td>
                <td className="py-1.5 px-2 text-right text-[#e7e9ea]">{cell("likes")}</td>
                <td className="py-1.5 px-2 text-right text-[#e7e9ea]">{cell("reposts")}</td>
                <td className="py-1.5 px-2 text-right text-[#e7e9ea]">{cell("replies")}</td>
                <td className="py-1.5 px-2 text-right text-[#e7e9ea]">{cell("bookmarks")}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ── TweetCard ────────────────────────────────────────────────────────────────
export default function TweetCard({ tweet, onHideAuthor }: { tweet: Tweet; onHideAuthor?: (username: string) => void }) {
  const isRetweet = tweet.tweet_type === "retweet";
  const isQuote = tweet.tweet_type === "quote_tweet";

  // For retweets, display the original author; for others, the posting author
  const displayAuthorUsername = isRetweet
    ? (tweet.original_author_username ?? tweet.author_username)
    : tweet.author_username;

  const displayAuthorName = isRetweet
    ? (tweet.original_author_display_name ?? tweet.author_display_name)
    : tweet.author_display_name;

  const bodyText =
    isRetweet && tweet.original_author_username
      ? stripRtPrefix(tweet.text, tweet.original_author_username)
      : tweet.text;

  return (
    <article
      className="border-b px-4 py-3 hover:bg-white/[0.03] cursor-pointer transition-colors"
      style={{ borderColor: "#2f3336" }}
      onClick={() => window.open(tweet.tweet_url, "_blank")}
    >
      {/* ── Retweet header ── */}
      {isRetweet && (
        <div
          className="flex items-center gap-2 mb-2 ml-12 text-xs font-medium"
          style={{ color: "#71767b" }}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
            <path d="M4.5 3.88l4.432 4.14-1.364 1.46L5.5 7.55V16c0 1.1.896 2 2 2H13v2H7.5c-2.209 0-4-1.791-4-4V7.55L1.432 9.48.068 8.02 4.5 3.88zM16.5 6H11V4h5.5c2.209 0 4 1.791 4 4v8.45l2.068-1.93 1.364 1.46-4.432 4.14-4.432-4.14 1.364-1.46 2.068 1.93V8c0-1.104-.896-2-2-2z" />
          </svg>
          <span>{tweet.author_display_name || tweet.author_username} Retweeted</span>
          <span>·</span>
          <AbsoluteTime iso={String(tweet.created_at)} className="flex-shrink-0" />
        </div>
      )}

      <div className="flex gap-3">
        {/* Avatar */}
        <div className="flex flex-col items-center">
          <Avatar
            username={displayAuthorUsername}
            displayName={displayAuthorName}
            size={40}
          />
        </div>

        {/* Body */}
        <div className="flex-1 min-w-0">
          {/* Name · handle · time (time only for non-retweets; retweets show it in the header) */}
          <div className="flex items-baseline gap-1 flex-wrap leading-tight">
            <span className="font-bold text-[#e7e9ea] text-sm hover:underline">
              {displayAuthorName || displayAuthorUsername}
            </span>
            <span className="text-[#71767b] text-sm">
              @{displayAuthorUsername}
            </span>
            {!isRetweet && (
              <>
                <span className="text-[#71767b] text-sm">·</span>
                <AbsoluteTime iso={String(tweet.created_at)} className="text-[#71767b] text-sm flex-shrink-0" />
              </>
            )}
            {/* Hide author button */}
            {onHideAuthor && (
              <button
                className="ml-auto p-1 rounded-full text-[#71767b] hover:text-[#f4212e] hover:bg-[#f4212e]/10 transition-colors"
                title={`Hide @${tweet.author_username}'s tweets`}
                onClick={(e) => {
                  e.stopPropagation();
                  onHideAuthor(tweet.author_username);
                }}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
                  <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
                  <line x1="1" y1="1" x2="23" y2="23" />
                </svg>
              </button>
            )}
          </div>

          {/* Tweet body text */}
          <p className="text-[#e7e9ea] text-sm mt-1 leading-relaxed whitespace-pre-wrap break-words">
            <TweetText text={bodyText} />
          </p>

          {/* Quote tweet preview card */}
          {isQuote && tweet.quoted_tweet_url && (
            <div
              className="mt-3 rounded-2xl border p-3 hover:bg-white/[0.03] transition-colors"
              style={{ borderColor: "#2f3336" }}
              onClick={(e) => {
                e.stopPropagation();
                window.open(tweet.quoted_tweet_url!, "_blank");
              }}
            >
              <div className="flex items-center gap-1.5 mb-1">
                <svg viewBox="0 0 24 24" width="14" height="14" fill="#71767b">
                  <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.401 6.231H2.748l7.73-8.835L1.254 2.25H8.08l4.258 5.63 5.906-5.63z" />
                </svg>
                <span className="text-[#71767b] text-xs">
                  View quoted tweet
                </span>
              </div>
              <p className="text-[#71767b] text-xs break-all">
                {tweet.quoted_tweet_url}
              </p>
            </div>
          )}

          {/* Images */}
          {tweet.image_urls && tweet.image_urls.length > 0 && (
            <div className="mt-3 rounded-2xl overflow-hidden">
              <img
                src={tweet.image_urls[0]}
                alt="Tweet image"
                className="w-full object-cover rounded-2xl"
                style={{ maxHeight: 300 }}
              />
            </div>
          )}

          {/* Engagement snapshots (15m, 1h, 12h) */}
          {tweet.snapshots && tweet.snapshots.length > 0 && (
            <SnapshotTable snapshots={tweet.snapshots} />
          )}
        </div>
      </div>
    </article>
  );
}
