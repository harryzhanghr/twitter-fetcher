import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import type { Tweet, EngagementSnapshot } from "@/lib/types";

const PAGE_SIZE = 40;

export async function GET(req: NextRequest) {
  const cursor = req.nextUrl.searchParams.get("cursor"); // ISO timestamp
  const sql = getDb();

  let rows: Tweet[];
  if (cursor) {
    rows = await sql<Tweet[]>`
      SELECT
        tweet_id, tweet_type, tweet_url, text,
        author_username, author_display_name,
        original_author_username, original_author_display_name,
        quoted_tweet_url, embedded_urls, image_urls,
        created_at, fetched_at,
        views, likes, reposts, quotes, replies, bookmarks
      FROM fetched_tweets
      WHERE created_at < ${cursor}
      ORDER BY created_at DESC
      LIMIT ${PAGE_SIZE}
    `;
  } else {
    rows = await sql<Tweet[]>`
      SELECT
        tweet_id, tweet_type, tweet_url, text,
        author_username, author_display_name,
        original_author_username, original_author_display_name,
        quoted_tweet_url, embedded_urls, image_urls,
        created_at, fetched_at,
        views, likes, reposts, quotes, replies, bookmarks
      FROM fetched_tweets
      ORDER BY created_at DESC
      LIMIT ${PAGE_SIZE}
    `;
  }

  // Attach engagement snapshots
  const tweetIds = rows.map((r) => r.tweet_id);
  if (tweetIds.length > 0) {
    const snaps = await sql<(EngagementSnapshot & { tweet_id: string })[]>`
      SELECT tweet_id, label, captured_at, views, likes, reposts, quotes, replies, bookmarks
      FROM engagement_snapshots
      WHERE tweet_id = ANY(${tweetIds})
      ORDER BY captured_at ASC
    `;

    const snapMap = new Map<string, EngagementSnapshot[]>();
    for (const s of snaps) {
      const arr = snapMap.get(s.tweet_id) || [];
      arr.push({
        label: s.label,
        captured_at: s.captured_at,
        views: s.views,
        likes: s.likes,
        reposts: s.reposts,
        quotes: s.quotes,
        replies: s.replies,
        bookmarks: s.bookmarks,
      });
      snapMap.set(s.tweet_id, arr);
    }

    for (const tweet of rows) {
      tweet.snapshots = snapMap.get(tweet.tweet_id);
    }
  }

  const nextCursor =
    rows.length === PAGE_SIZE
      ? String(rows[rows.length - 1].created_at)
      : null;

  return NextResponse.json({ tweets: rows, nextCursor });
}
