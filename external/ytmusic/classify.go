package ytmusic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bjarneo/cliamp/internal/appdir"

	"google.golang.org/api/youtube/v3"
)

// musicCategoryID is the YouTube video category for Music.
const musicCategoryID = "10"

// classifySampleSize is how many playlist items are sampled to decide whether a
// playlist is music. Sampling a single item made one unavailable or off-genre
// video decide an entire playlist; videos.list costs the same quota either way.
const classifySampleSize = 10

// classificationCache maps playlist ID → true if the playlist is music.
type classificationCache struct {
	Scope string          `json:"scope"`
	Music map[string]bool `json:"music"` // playlist ID → is music
}

// classificationCachePath returns the path to the classification cache file.
func classificationCachePath() string {
	dir, err := appdir.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ytmusic_classification.json")
}

// loadClassification loads cached playlist classifications from disk.
func loadClassification(scope string) map[string]bool {
	data, err := os.ReadFile(classificationCachePath())
	if err != nil {
		return nil
	}
	var cache classificationCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	if cache.Scope != scope {
		return nil
	}
	return cache.Music
}

// saveClassification writes playlist classifications to disk.
func saveClassification(scope string, music map[string]bool) {
	cache := classificationCache{Scope: scope, Music: music}
	data, _ := json.MarshalIndent(cache, "", "  ")
	path := classificationCachePath()
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, data, 0o600)
}

// classifyPlaylists determines which playlists contain music content by
// sampling one video from each and checking its category.
// Returns a map of playlist ID → true (music) / false (not music).
// Results are cached to disk to avoid repeated API calls.
func classifyPlaylists(ctx context.Context, svc *youtube.Service, playlists []playlistEntry, existing map[string]bool, scope string, inYTM map[string]bool) map[string]bool {
	cached := existing
	if cached == nil {
		cached = loadClassification(scope)
	}
	if cached == nil {
		cached = make(map[string]bool)
	}

	// Find playlists that need classification. Membership of the YouTube Music
	// library settles it without sampling: the user filed it under music there.
	var toClassify []playlistEntry
	for _, pl := range playlists {
		if inYTM[pl.ID] {
			continue
		}
		if _, ok := cached[pl.ID]; !ok {
			toClassify = append(toClassify, pl)
		}
	}

	if len(toClassify) == 0 {
		return cached
	}

	// Sample several video IDs from each playlist (parallel, max 10 concurrent).
	type sampleResult struct {
		playlistID string
		videoIDs   []string
	}
	sampleCh := make(chan sampleResult, len(toClassify))
	sem := make(chan struct{}, 10) // concurrency limit
	var wg sync.WaitGroup

	for _, pl := range toClassify {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := svc.PlaylistItems.List([]string{"contentDetails"}).
				PlaylistId(pl.ID).
				MaxResults(classifySampleSize).
				Context(ctx).
				Do()
			if err != nil {
				sampleCh <- sampleResult{playlistID: pl.ID}
				return
			}
			var ids []string
			for _, item := range resp.Items {
				if vid := item.ContentDetails.VideoId; vid != "" {
					ids = append(ids, vid)
				}
			}
			sampleCh <- sampleResult{playlistID: pl.ID, videoIDs: ids}
		})
	}

	wg.Wait()
	close(sampleCh)

	// Collect video IDs for batch category lookup. Unavailable videos (private,
	// deleted) simply do not come back from videos.list and so never sway the
	// vote; a playlist with no readable sample defaults to non-music.
	videoToPlaylist := make(map[string]string) // videoID → playlistID
	var videoIDs []string
	for s := range sampleCh {
		if len(s.videoIDs) == 0 {
			cached[s.playlistID] = false
			continue
		}
		for _, vid := range s.videoIDs {
			videoToPlaylist[vid] = s.playlistID
			videoIDs = append(videoIDs, vid)
		}
	}

	// Batch fetch video categories and tally votes per playlist.
	musicVotes := make(map[string]int)
	totalVotes := make(map[string]int)
	for i := 0; i < len(videoIDs); i += youtubeAPIBatchSize {
		end := min(i+youtubeAPIBatchSize, len(videoIDs))
		batch := videoIDs[i:end]

		vResp, err := svc.Videos.List([]string{"snippet"}).
			Id(batch...).
			Context(ctx).
			Do()
		if err != nil {
			continue
		}

		for _, v := range vResp.Items {
			plID := videoToPlaylist[v.Id]
			totalVotes[plID]++
			if v.Snippet.CategoryId == musicCategoryID {
				musicVotes[plID]++
			}
		}
	}

	// A playlist is music when most of its readable sample is music.
	for plID, total := range totalVotes {
		cached[plID] = musicVotes[plID]*2 >= total
	}

	// Mark any remaining unclassified as non-music.
	for _, pl := range toClassify {
		if _, ok := cached[pl.ID]; !ok {
			cached[pl.ID] = false
		}
	}

	saveClassification(scope, cached)
	return cached
}

// playlistEntry is a minimal playlist descriptor for classification.
type playlistEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TrackCount int    `json:"track_count"`
}

// classifyWithTimeout runs classification with a timeout.
func classifyWithTimeout(svc *youtube.Service, playlists []playlistEntry, timeout time.Duration, existing map[string]bool, scope string, inYTM map[string]bool) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	classified := classifyPlaylists(ctx, svc, playlists, existing, scope, inYTM)
	// Applied after saveClassification so the disk cache keeps only
	// category-derived verdicts and the override stays re-appliable.
	for id := range inYTM {
		classified[id] = true
	}
	return classified
}
