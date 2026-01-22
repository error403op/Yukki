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

func (y *YtdlpPlatform) CanDownload(source state.PlatformName) bool {
	return source == y.name || source == PlatformYouTube
}

func (y *YtdlpPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {

	if f := findFile(track); f != "" {
		return f, nil
	}

	args := []string{
		"--no-playlist",
		"--no-part",
		"--geo-bypass",
		"--no-check-certificate",
		"--print", "after_move:filepath",
		"-o", getPath(track, ".%(ext)s"),
	}

	if track.Video {
		args = append(args,
			"-f", "bv*[height<=1080]+ba/b",
		)
	} else {
		args = append(args,
			"-f", "ba/b",
			"-x",
			"--audio-format", "mp3",
			"--audio-quality", "0",
			"--concurrent-fragments", "4",
		)
	}

	if y.isYouTubeURL(track.URL) {
		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil && cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
			gologging.InfoF("YtDlp: Using cookies: %s", cookieFile)
		}
	}

	args = append(args, track.URL)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	gologging.InfoF("YtDlp CMD: yt-dlp %s", strings.Join(args, " "))

	err := cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	errStr := strings.TrimSpace(stderr.String())

	// Any yt-dlp failure: log everything, but NEVER expose internals to Telegram
	if err != nil {
		gologging.ErrorF(
			"YtDlp FAILED\nTitle: %s\nURL: %s\nERROR: %v\nSTDOUT:\n%s\nSTDERR:\n%s",
			track.Title, track.URL, err, outStr, errStr,
		)

		// Detect YouTube signature / challenge solver problem
		if strings.Contains(errStr, "Signature solving failed") ||
			strings.Contains(errStr, "challenge solving failed") {
			gologging.Error(
				"YtDlp: YouTube signature challenge failed. " +
					"Install EJS runtime or Node.js.\n" +
					"See: https://github.com/yt-dlp/yt-dlp/wiki/EJS",
			)
		}

		findAndRemove(track)

		// Clean message only
		return "", errors.New("failed to download this media, try again later or use another source")
	}

	if outStr == "" {
		gologging.Error("YtDlp: yt-dlp returned empty output path")
		findAndRemove(track)
		return "", errors.New("download failed")
	}

	// Check empty file case
	if fileInfo, err := os.Stat(outStr); err == nil {
		if fileInfo.Size() == 0 {
			gologging.ErrorF("YtDlp: Downloaded file is empty: %s", outStr)
			findAndRemove(track)
			return "", errors.New("download failed")
		}
	} else {
		gologging.ErrorF("YtDlp: Cannot stat downloaded file: %s : %v", outStr, err)
		findAndRemove(track)
		return "", errors.New("download failed")
	}

	gologging.InfoF("YtDlp: Successfully downloaded %s", outStr)
	return outStr, nil
}

func (*YtdlpPlatform) CanSearch() bool { return false }

func (*YtdlpPlatform) Search(string, bool) ([]*state.Track, error) {
	return nil, nil
}

func (y *YtdlpPlatform) extractMetadata(urlStr string) (*ytdlpInfo, error) {
	args := []string{
		"-j",
		"--no-check-certificate",
	}

	if y.isYouTubeURL(urlStr) {
		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil && cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
		}
	}

	args = append(args, urlStr)

	cmd := exec.Command("yt-dlp", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		gologging.ErrorF(
			"YtDlp Metadata Error\nURL: %s\nERROR: %v\nSTDERR:\n%s",
			urlStr, err, stderr.String(),
		)
		return nil, errors.New("failed to extract media information")
	}

	var info ytdlpInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		gologging.ErrorF("YtDlp JSON parse error: %v\nRAW:\n%s", err, stdout.String())
		return nil, errors.New("failed to parse media information")
	}

	return &info, nil
}

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

func (y *YtdlpPlatform) isYouTubeURL(urlStr string) bool {
	for _, pattern := range youtubePatterns {
		if pattern.MatchString(urlStr) {
			return true
		}
	}
	return false
}
