package platforms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
			"-f",
			"bv*[height<=1080]+ba/b",
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

	if err != nil {
		gologging.ErrorF(
			"YtDlp FAILED\nURL: %s\nERROR: %v\nSTDOUT:\n%s\nSTDERR:\n%s",
			track.URL, err, outStr, errStr,
		)
		findAndRemove(track)
		return "", fmt.Errorf("yt-dlp error:\n%s", errStr)
	}

	if outStr == "" {
		return "", errors.New("yt-dlp did not return downloaded file path")
	}

	gologging.InfoF("YtDlp Downloaded: %s", outStr)
	return outStr, nil
}

func (*YtdlpPlatform) CanSearch() bool { return false }

func (*YtdlpPlatform) Search(string, bool) ([]*state.Track, error) {
	return nil, nil
}

func (y *YtdlpPlatform) extractMetadata(urlStr string) (*ytdlpInfo, error) {
	args := []string{
		"-j",
		"--no-warnings",
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
		return nil, fmt.Errorf("yt-dlp metadata error:\n%s", stderr.String())
	}

	var info ytdlpInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, err
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
