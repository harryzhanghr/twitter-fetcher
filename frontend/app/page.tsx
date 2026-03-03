import { unstable_noStore as noStore } from "next/cache";
import { getDb } from "@/lib/db";
import Feed from "@/components/Feed";
import type { Tweet, EngagementSnapshot } from "@/lib/types";

async function getTweets(): Promise<Tweet[]> {
  noStore(); // Opt out of static generation — fetch fresh data at runtime.
  const sql = getDb();
  const rows = await sql<Tweet[]>`
    SELECT
      tweet_id,
      tweet_type,
      tweet_url,
      text,
      author_username,
      author_display_name,
      original_author_username,
      original_author_display_name,
      quoted_tweet_url,
      embedded_urls,
      image_urls,
      created_at,
      fetched_at,
      views,
      likes,
      reposts,
      quotes,
      replies,
      bookmarks
    FROM fetched_tweets
    ORDER BY created_at DESC
    LIMIT 40
  `;

  // Fetch engagement snapshots for these tweets
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

  return rows;
}

export default async function Home() {
  const tweets = await getTweets();

  return (
    <main className="min-h-screen bg-black">
      {/* Centered feed column — mirrors Twitter's layout */}
      <div
        className="max-w-[600px] mx-auto border-x"
        style={{ borderColor: "#2f3336" }}
      >
        {/* Sticky header */}
        <div
          className="sticky top-0 z-10 px-4 py-3 border-b flex items-center gap-4"
          style={{
            backgroundColor: "rgba(0,0,0,0.85)",
            backdropFilter: "blur(12px)",
            borderColor: "#2f3336",
          }}
        >
          {/* X logo */}
          <svg viewBox="0 0 24 24" width="24" height="24" fill="white">
            <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.401 6.231H2.748l7.73-8.835L1.254 2.25H8.08l4.258 5.63 5.906-5.63zm-1.161 17.52h1.833L7.084 4.126H5.117L17.083 19.77z" />
          </svg>
          <h1 className="text-lg font-bold text-[#e7e9ea]">Feed</h1>
          <span className="ml-auto text-xs text-[#71767b]">
            {tweets.length} tweet{tweets.length !== 1 ? "s" : ""}
          </span>
        </div>

        <Feed tweets={tweets} />
      </div>
    </main>
  );
}
