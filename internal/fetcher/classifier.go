package fetcher

import (
	"fmt"

	"github.com/harryz/twitter-fetcher/internal/db"
	"github.com/harryz/twitter-fetcher/internal/twitter"
)

// ClassifyResult holds the outcome of classifying a single tweet.
type ClassifyResult struct {
	TweetType                  db.TweetType
	TweetURL                   string
	QuotedTweetURL             string
	OriginalAuthorUsername     string
	OriginalAuthorDisplayName  string
	// FullText is set for retweets to the original tweet's untruncated text.
	// Empty for quote tweets — the poster's own text is already complete.
	FullText                   string
	EmbeddedURLs               []string
	ImageURLs                  []string
	VideoURLs                  []string
}

// BuildLookupMaps builds O(1) maps from the API includes payload.
func BuildLookupMaps(includes twitter.Includes) (
	tweetMap map[string]twitter.Tweet,
	userMap map[string]twitter.UserInfo,
	mediaMap map[string]twitter.Media,
) {
	tweetMap = make(map[string]twitter.Tweet, len(includes.Tweets))
	for _, t := range includes.Tweets {
		tweetMap[t.ID] = t
	}
	userMap = make(map[string]twitter.UserInfo, len(includes.Users))
	for _, u := range includes.Users {
		userMap[u.ID] = u
	}
	mediaMap = make(map[string]twitter.Media, len(includes.Media))
	for _, m := range includes.Media {
		mediaMap[m.MediaKey] = m
	}
	return
}

// ClassifyTweet determines whether a tweet is a quote tweet or retweet,
// resolves URLs, and extracts media. Returns ok=false if neither.
func ClassifyTweet(
	tweet twitter.Tweet,
	authorUser twitter.UserInfo,
	tweetMap map[string]twitter.Tweet,
	userMap map[string]twitter.UserInfo,
	mediaMap map[string]twitter.Media,
) (result ClassifyResult, ok bool) {
	result.EmbeddedURLs = extractURLs(tweet)
	result.ImageURLs, result.VideoURLs = extractMedia(tweet, mediaMap)

	for _, ref := range tweet.ReferencedTweets {
		switch ref.Type {
		case "quoted":
			result.TweetType = db.TweetTypeQuoteTweet
			result.TweetURL = fmt.Sprintf("https://twitter.com/%s/status/%s", authorUser.Username, tweet.ID)

			// Resolve the URL of the tweet being quoted.
			if origTweet, found := tweetMap[ref.ID]; found {
				if origUser, found := userMap[origTweet.AuthorID]; found {
					result.QuotedTweetURL = fmt.Sprintf("https://twitter.com/%s/status/%s", origUser.Username, ref.ID)
				} else {
					result.QuotedTweetURL = fmt.Sprintf("https://twitter.com/unknown/status/%s", ref.ID)
				}
			} else {
				result.QuotedTweetURL = fmt.Sprintf("https://twitter.com/unknown/status/%s", ref.ID)
			}
			return result, true

		case "retweeted":
			origTweet, found := tweetMap[ref.ID]
			if !found {
				result.TweetType = db.TweetTypeRetweet
				result.TweetURL = fmt.Sprintf("https://twitter.com/unknown/status/%s", ref.ID)
				return result, true
			}

			origUser, found := userMap[origTweet.AuthorID]
			if !found {
				result.TweetType = db.TweetTypeRetweet
				result.TweetURL = fmt.Sprintf("https://twitter.com/unknown/status/%s", ref.ID)
				return result, true
			}

			result.TweetType = db.TweetTypeRetweet
			result.TweetURL = fmt.Sprintf("https://twitter.com/%s/status/%s", origUser.Username, ref.ID)
			result.OriginalAuthorUsername = origUser.Username
			result.OriginalAuthorDisplayName = origUser.Name
			// Use the full untruncated text from the referenced tweet object.
			result.FullText = origTweet.Text
			return result, true
		}
	}
	// No referenced tweets → original tweet (not a reply, not a RT, not a quote).
	if len(tweet.ReferencedTweets) == 0 {
		result.TweetType = db.TweetTypeTweet
		result.TweetURL = fmt.Sprintf("https://twitter.com/%s/status/%s", authorUser.Username, tweet.ID)
		return result, true
	}

	// Has referenced tweets but none matched (e.g. reply-only) → skip.
	return ClassifyResult{}, false
}

func extractURLs(tweet twitter.Tweet) []string {
	if len(tweet.Entities.URLs) == 0 {
		return nil
	}
	urls := make([]string, 0, len(tweet.Entities.URLs))
	for _, u := range tweet.Entities.URLs {
		if u.ExpandedURL != "" {
			urls = append(urls, u.ExpandedURL)
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return urls
}

func extractMedia(tweet twitter.Tweet, mediaMap map[string]twitter.Media) (imageURLs, videoURLs []string) {
	for _, key := range tweet.Attachments.MediaKeys {
		m, ok := mediaMap[key]
		if !ok {
			continue
		}
		switch m.Type {
		case "photo":
			if m.URL != "" {
				imageURLs = append(imageURLs, m.URL)
			}
		case "video", "animated_gif":
			if best := bestVideoURL(m.Variants); best != "" {
				videoURLs = append(videoURLs, best)
			} else if m.PreviewImageURL != "" {
				videoURLs = append(videoURLs, m.PreviewImageURL)
			}
		}
	}
	return
}

// bestVideoURL picks the highest-bitrate mp4 variant.
func bestVideoURL(variants []twitter.MediaVariant) string {
	var best string
	var bestBitRate int
	for _, v := range variants {
		if v.ContentType == "video/mp4" && v.BitRate >= bestBitRate {
			best = v.URL
			bestBitRate = v.BitRate
		}
	}
	return best
}
