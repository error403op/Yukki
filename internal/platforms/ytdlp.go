package platforms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/cookies"
	state "main/internal/core/models"
)

const PlatformYtDlp state.PlatformName = "YtDlp"

type YtdlpPlatform struct {
	name state.PlatformName
}

type ytdlpInfo struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Duration    float64     `json:"duration"`
	Thumbnail   string      `json:"thumbnail"`
	URL         string      `json:"webpage_url"`
	OriginalURL string      `json:"original_url"`
	Uploader    string      `json:"uploader"`
	Description string      `json:"description"`
	IsLive      bool        `json:"is_live"`
	Entries     []ytdlpInfo `json:"entries"`
}

var youtubePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(youtube\.com|youtu\.be|music\.youtube\.com)`),
}

// 🔥 Runtime priority (DO NOT reorder)
var jsRuntimes = []string{
	"node",
	"bun",
	"deno",
}

// 🔥 Elite player chain
const ytPlayerClients = "youtube:player_client=android,tv,web,ios"

func init() {
	Register(60, &YtdlpPlatform{
		name: PlatformYtDlp,
	})
}

func (y *YtdlpPlatform) Name() state.PlatformName {
	return y.name
}

func (y *YtdlpPlatform) CanGetTracks(query string) bool {
	query = strings.TrimSpace(query)

	parsedURL, err := url.Parse(query)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}
	return true
}

//////////////////////////////////////////////////////////////

func (y *YtdlpPlatform) GetTracks(query string, video bool) ([]*state.Track, error) {

	gologging.InfoF("YtDlp: Extracting metadata for %s", query)

	info, err := y.extractMetadata(query)
	if err != nil {
		return nil, err
	}

	if info.IsLive {
		return nil, errors.New("live streams are not supported")
	}

	var tracks []*state.Track

	if len(info.Entries) > 0 {
		for _, entry := range info.Entries {
			if entry.IsLive {
				continue
			}
			tracks = append(tracks, y.infoToTrack(&entry, video))
		}
	} else {
		tracks = append(tracks, y.infoToTrack(info, video))
	}

	return tracks, nil
}

//////////////////////////////////////////////////////////////

func (y *YtdlpPlatform) CanDownload(source state.PlatformName) bool {
	return source == y.name || source == PlatformYouTube
}

//////////////////////////////////////////////////////////////
// 🔥 RUNTIME EXECUTOR (CORE UPGRADE)
//////////////////////////////////////////////////////////////

func runWithRuntime(ctx context.Context, runtime string, args []string) (string, string, error) {

	fullArgs := append([]string{"--js-runtime", runtime}, args...)

	cmd := exec.CommandContext(ctx, "yt-dlp", fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return strings.TrimSpace(stdout.String()),
		strings.TrimSpace(stderr.String()),
		err
}

//////////////////////////////////////////////////////////////
// DOWNLOAD
//////////////////////////////////////////////////////////////

func (y *YtdlpPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {

	if f := findFile(track); f != "" {
		return f, nil
	}

	baseArgs := []string{
		"--no-part",
		"--ignore-errors",
		"--no-warnings",

		"--extractor-retries", "5",
		"--fragment-retries", "5",
		"--file-access-retries", "5",

		"--socket-timeout", "20",
		"--force-ipv4",

		"--concurrent-fragments", "4",
		"--throttled-rate", "100K",

		"--max-filesize", "2G",

		"--print", "after_move:filepath",
		"-o", getPath(track, ".%(ext)s"),
	}

	if y.isYouTubeURL(track.URL) {

		baseArgs = append(baseArgs,
			"--extractor-args",
			ytPlayerClients,
		)

		if track.Video {
			baseArgs = append(baseArgs,
				"-f", "bv*[height<=1080]+ba/b",
			)
		} else {
			baseArgs = append(baseArgs,
				"-f", "ba[ext=m4a]/ba/bestaudio",
				"-x",
				"--audio-format", "mp3",
				"--audio-quality", "0",
			)
		}

		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil && cookieFile != "" {
			baseArgs = append(baseArgs, "--cookies", cookieFile)
		}

	} else {

		if track.Video {
			baseArgs = append(baseArgs,
				"-f", "bestvideo+bestaudio/best",
			)
		} else {
			baseArgs = append(baseArgs,
				"-f", "bestaudio/best",
			)
		}
	}

	baseArgs = append(baseArgs, track.URL)

	//////////////////////////////////////////////////////////////
	// 🔥 RUNTIME FALLBACK LOOP
	//////////////////////////////////////////////////////////////

	for _, runtime := range jsRuntimes {

		gologging.InfoF("YtDlp attempting runtime: %s", runtime)

		out, errStr, err := runWithRuntime(ctx, runtime, baseArgs)

		if err == nil && out != "" {

			fileInfo, statErr := os.Stat(out)
			if statErr == nil && fileInfo.Size() > 0 {

				gologging.InfoF("YtDlp SUCCESS with runtime %s → %s", runtime, out)
				return out, nil
			}
		}

		gologging.WarnF("Runtime %s failed: %v\n%s", runtime, err, errStr)
	}

	findAndRemove(track)

	return "", errors.New("all JS runtimes failed to download media")
}

//////////////////////////////////////////////////////////////

func (*YtdlpPlatform) CanSearch() bool { return false }

func (*YtdlpPlatform) Search(string, bool) ([]*state.Track, error) {
	return nil, nil
}

//////////////////////////////////////////////////////////////
// METADATA WITH FALLBACK
//////////////////////////////////////////////////////////////

func (y *YtdlpPlatform) extractMetadata(urlStr string) (*ytdlpInfo, error) {

	baseArgs := []string{
		"--dump-single-json",
		"--no-playlist",
		"--extractor-retries", "5",
	}

	if y.isYouTubeURL(urlStr) {

		baseArgs = append(baseArgs,
			"--extractor-args",
			ytPlayerClients,
		)

		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil && cookieFile != "" {
			baseArgs = append(baseArgs, "--cookies", cookieFile)
		}
	}

	baseArgs = append(baseArgs, urlStr)

	for _, runtime := range jsRuntimes {

		gologging.InfoF("Metadata attempting runtime: %s", runtime)

		out, errStr, err := runWithRuntime(context.Background(), runtime, baseArgs)

		if err == nil && out != "" {

			var info ytdlpInfo

			if jsonErr := json.Unmarshal([]byte(out), &info); jsonErr == nil {
				return &info, nil
			}
		}

		gologging.WarnF("Metadata runtime %s failed: %v\n%s", runtime, err, errStr)
	}

	return nil, errors.New("failed to extract media information (all runtimes failed)")
}

//////////////////////////////////////////////////////////////

func (y *YtdlpPlatform) infoToTrack(info *ytdlpInfo, video bool) *state.Track {

	trackURL := info.URL
	if info.OriginalURL != "" {
		trackURL = info.OriginalURL
	}

	return &state.Track{
		ID:       info.ID,
		Title:    info.Title,
		Duration: int(info.Duration),
		Artwork:  info.Thumbnail,
		URL:      trackURL,
		Source:   PlatformYtDlp,
		Video:    video,
	}
}

//////////////////////////////////////////////////////////////

func (y *YtdlpPlatform) isYouTubeURL(urlStr string) bool {
	for _, pattern := range youtubePatterns {
		if pattern.MatchString(urlStr) {
			return true
		}
	}
	return false
}
