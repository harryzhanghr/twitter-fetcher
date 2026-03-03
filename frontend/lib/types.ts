export interface EngagementSnapshot {
  label: string;
  captured_at: string;
  views: number;
  likes: number;
  reposts: number;
  quotes: number;
  replies: number;
  bookmarks: number;
}

export interface Tweet {
  tweet_id: string;
  tweet_type: "tweet" | "retweet" | "quote_tweet";
  tweet_url: string;
  text: string;
  author_username: string;
  author_display_name: string | null;
  original_author_username: string | null;
  original_author_display_name: string | null;
  quoted_tweet_url: string | null;
  embedded_urls: string[] | null;
  image_urls: string[] | null;
  created_at: string;
  fetched_at: string;
  views: number | null;
  likes: number | null;
  reposts: number | null;
  quotes: number | null;
  replies: number | null;
  bookmarks: number | null;
  snapshots?: EngagementSnapshot[];
}
