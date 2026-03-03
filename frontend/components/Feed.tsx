"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import type { Tweet } from "@/lib/types";
import TweetCard from "./TweetCard";

type Filter = "no_retweets" | "all";

const BLACKLIST_KEY = "blacklistedAuthors";

function loadBlacklist(): Set<string> {
  if (typeof window === "undefined") return new Set();
  try {
    const raw = localStorage.getItem(BLACKLIST_KEY);
    return raw ? new Set(JSON.parse(raw)) : new Set();
  } catch {
    return new Set();
  }
}

function saveBlacklist(set: Set<string>) {
  localStorage.setItem(BLACKLIST_KEY, JSON.stringify([...set]));
}

export default function Feed({ tweets: initial }: { tweets: Tweet[] }) {
  const [filter, setFilter] = useState<Filter>("no_retweets");
  const [tweets, setTweets] = useState<Tweet[]>(initial);
  const [cursor, setCursor] = useState<string | null>(
    initial.length > 0 ? String(initial[initial.length - 1].created_at) : null
  );
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(initial.length >= 40);
  const [blacklist, setBlacklist] = useState<Set<string>>(new Set());
  const [showBlacklist, setShowBlacklist] = useState(false);
  const sentinel = useRef<HTMLDivElement>(null);

  // Load blacklist from localStorage on mount
  useEffect(() => {
    setBlacklist(loadBlacklist());
  }, []);

  const hideAuthor = useCallback((username: string) => {
    setBlacklist((prev) => {
      const next = new Set(prev);
      next.add(username.toLowerCase());
      saveBlacklist(next);
      return next;
    });
  }, []);

  const unhideAuthor = useCallback((username: string) => {
    setBlacklist((prev) => {
      const next = new Set(prev);
      next.delete(username.toLowerCase());
      saveBlacklist(next);
      return next;
    });
  }, []);

  const loadMore = useCallback(async () => {
    if (loading || !hasMore || !cursor) return;
    setLoading(true);
    try {
      const res = await fetch(`/api/tweets?cursor=${encodeURIComponent(cursor)}`);
      const data = await res.json();
      const newTweets: Tweet[] = data.tweets;
      if (newTweets.length === 0) {
        setHasMore(false);
      } else {
        setTweets((prev) => [...prev, ...newTweets]);
        setCursor(data.nextCursor);
        if (!data.nextCursor) setHasMore(false);
      }
    } catch (err) {
      console.error("Failed to load more tweets:", err);
    } finally {
      setLoading(false);
    }
  }, [loading, hasMore, cursor]);

  // IntersectionObserver to trigger loadMore when sentinel is visible
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) {
          loadMore();
        }
      },
      { rootMargin: "400px" }
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [loadMore]);

  const isHidden = (t: Tweet) => {
    if (blacklist.has(t.author_username.toLowerCase())) return true;
    if (t.original_author_username && blacklist.has(t.original_author_username.toLowerCase())) return true;
    return false;
  };

  let filtered = tweets.filter((t) => !isHidden(t));
  if (filter === "no_retweets") {
    filtered = filtered.filter((t) => t.tweet_type !== "retweet");
  }

  if (tweets.length === 0 && !loading) {
    return (
      <div className="flex flex-col items-center justify-center py-20 gap-3">
        <p className="text-[#71767b] text-lg">No tweets yet</p>
        <p className="text-[#71767b] text-sm">
          The fetcher hasn&apos;t collected any tweets yet.
        </p>
      </div>
    );
  }

  return (
    <div>
      {/* Filter tabs */}
      <div className="flex border-b" style={{ borderColor: "#2f3336" }}>
        <button
          className="flex-1 py-3 text-sm font-medium text-center transition-colors relative"
          style={{ color: filter === "no_retweets" ? "#e7e9ea" : "#71767b" }}
          onClick={() => setFilter("no_retweets")}
        >
          Without Retweets
          {filter === "no_retweets" && (
            <div className="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 rounded-full bg-[#1d9bf0]" />
          )}
        </button>
        <button
          className="flex-1 py-3 text-sm font-medium text-center transition-colors relative"
          style={{ color: filter === "all" ? "#e7e9ea" : "#71767b" }}
          onClick={() => setFilter("all")}
        >
          All
          {filter === "all" && (
            <div className="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 rounded-full bg-[#1d9bf0]" />
          )}
        </button>

        {/* Blacklist badge */}
        {blacklist.size > 0 && (
          <button
            className="flex items-center gap-1.5 px-3 py-3 text-xs font-medium transition-colors"
            style={{ color: "#f4212e" }}
            onClick={() => setShowBlacklist((v) => !v)}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
              <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
              <line x1="1" y1="1" x2="23" y2="23" />
            </svg>
            {blacklist.size} hidden
          </button>
        )}
      </div>

      {/* Blacklist management panel */}
      {showBlacklist && blacklist.size > 0 && (
        <div
          className="border-b px-4 py-3"
          style={{ borderColor: "#2f3336", backgroundColor: "#16181c" }}
        >
          <p className="text-[#71767b] text-xs mb-2">Hidden accounts (click to unhide):</p>
          <div className="flex flex-wrap gap-2">
            {[...blacklist].sort().map((username) => (
              <button
                key={username}
                className="flex items-center gap-1 px-2 py-1 rounded-full text-xs transition-colors hover:bg-white/10"
                style={{ backgroundColor: "#2f3336", color: "#e7e9ea" }}
                onClick={() => unhideAuthor(username)}
              >
                @{username}
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                  <line x1="18" y1="6" x2="6" y2="18" />
                  <line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Tweet list */}
      {filtered.map((tweet) => (
        <TweetCard key={tweet.tweet_id} tweet={tweet} onHideAuthor={hideAuthor} />
      ))}

      {/* Sentinel + loading indicator */}
      <div ref={sentinel} className="py-6 flex justify-center">
        {loading && (
          <div className="flex items-center gap-2 text-[#71767b] text-sm">
            <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
            </svg>
            Loading...
          </div>
        )}
        {!hasMore && tweets.length > 0 && (
          <p className="text-[#71767b] text-sm">No more tweets</p>
        )}
      </div>

      {filtered.length === 0 && !loading && (
        <div className="flex flex-col items-center justify-center py-20 gap-3">
          <p className="text-[#71767b] text-lg">No tweets match this filter</p>
        </div>
      )}
    </div>
  );
}
