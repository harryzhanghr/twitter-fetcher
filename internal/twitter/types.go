package twitter

// PublicMetrics holds engagement counts for a tweet.
type PublicMetrics struct {
	RetweetCount    int `json:"retweet_count"`
	ReplyCount      int `json:"reply_count"`
	LikeCount       int `json:"like_count"`
	QuoteCount      int `json:"quote_count"`
	ImpressionCount int `json:"impression_count"`
	BookmarkCount   int `json:"bookmark_count"`
}

// ReferencedTweet is a tweet referenced by another (retweet, quote, reply).
type ReferencedTweet struct {
	Type string `json:"type"` // "retweeted", "quoted", "replied_to"
	ID   string `json:"id"`
}

// EntityURL is a URL extracted from tweet entities, with the t.co link resolved.
type EntityURL struct {
	URL         string `json:"url"`          // t.co short URL
	ExpandedURL string `json:"expanded_url"` // resolved destination URL
}

// Entities holds inline content extracted from tweet text.
type Entities struct {
	URLs []EntityURL `json:"urls"`
}

// Attachments lists media keys attached to a tweet.
type Attachments struct {
	MediaKeys []string `json:"media_keys"`
}

// MediaVariant is one encoding of a video or GIF.
type MediaVariant struct {
	ContentType string `json:"content_type"` // e.g. "video/mp4"
	URL         string `json:"url"`
	BitRate     int    `json:"bit_rate"`
}

// Media represents a photo, video, or animated GIF attached to a tweet.
type Media struct {
	MediaKey        string         `json:"media_key"`
	Type            string         `json:"type"` // "photo", "video", "animated_gif"
	URL             string         `json:"url"`               // set for photos
	PreviewImageURL string         `json:"preview_image_url"` // thumbnail for video/gif
	Variants        []MediaVariant `json:"variants"`          // video encoding options
}

// Tweet represents a Twitter API v2 tweet object.
type Tweet struct {
	ID               string            `json:"id"`
	Text             string            `json:"text"`
	AuthorID         string            `json:"author_id"`
	CreatedAt        string            `json:"created_at"`
	ReferencedTweets []ReferencedTweet `json:"referenced_tweets"`
	PublicMetrics    PublicMetrics     `json:"public_metrics"`
	ConversationID   string            `json:"conversation_id"`
	Attachments      Attachments       `json:"attachments"`
	Entities         Entities          `json:"entities"`
}

// UserInfo represents a Twitter API v2 user object.
type UserInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`     // display name, e.g. "Elon Musk"
	Username string `json:"username"` // handle, e.g. "elonmusk"
}

// Includes holds expanded objects returned alongside tweets.
type Includes struct {
	Tweets []Tweet    `json:"tweets"`
	Users  []UserInfo `json:"users"`
	Media  []Media    `json:"media"`
}

// Meta holds pagination metadata from the API response.
type Meta struct {
	NewestID    string `json:"newest_id"`
	OldestID    string `json:"oldest_id"`
	ResultCount int    `json:"result_count"`
	NextToken   string `json:"next_token"`
}

// UserTweetsResponse is the top-level response from GET /2/users/:id/tweets.
type UserTweetsResponse struct {
	Data     []Tweet  `json:"data"`
	Includes Includes `json:"includes"`
	Meta     Meta     `json:"meta"`
}

// UserTweetsRequest holds parameters for fetching a user's tweets.
type UserTweetsRequest struct {
	UserID     string
	SinceID    string // cursor: fetch tweets newer than this ID
	StartTime  string // ISO 8601; used on first run instead of SinceID
	MaxResults int
}

// TweetLookupResponse is the top-level response from GET /2/tweets?ids=...
type TweetLookupResponse struct {
	Data []Tweet `json:"data"`
}
