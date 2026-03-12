package platforms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"

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
	Register(60, &YtdlpPlatform{name: PlatformYtDlp})
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

	host := strings.ToLower(parsedURL.Host)

	if host == "t.me" ||
		host == "telegram.me" ||
		host == "telegram.dog" ||
		strings.HasSuffix(host, ".t.me") {
		return false
	}

	return true
}

func (y *YtdlpPlatform) GetTracks(query string, video bool) ([]*state.Track, error) {
	query = strings.TrimSpace(query)

	gologging.InfoF("YtDlp: Extracting metadata for %s", query)

	info, err := y.extractMetadata(query)
	if err != nil {
		gologging.ErrorF("YtDlp: Failed to extract metadata: %v", err)
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
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
		tracks = []*state.Track{y.infoToTrack(info, video)}
	}

	return tracks, nil
}

func (y *YtdlpPlatform) CanDownload(source state.PlatformName) bool {
	return source == y.name || source == PlatformYouTube
}

func logLarge(prefix, text string) {
	const chunk = 3000
	for len(text) > chunk {
		gologging.Error(prefix + text[:chunk])
		text = text[chunk:]
	}
	if len(text) > 0 {
		gologging.Error(prefix + text)
	}
}

func (y *YtdlpPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {

	if f := findFile(track); f != "" {
		gologging.Debug("Ytdlp: Cached File -> " + f)
		return f, nil
	}

	gologging.InfoF("YtDlp: Downloading %s", track.Title)

	args := []string{
		"--force-ipv4",
		"--no-playlist",
		"--no-part",
		"--geo-bypass",
		"--no-check-certificate",
		"--no-cache-dir",
		"--concurrent-fragments", "4",
		"--progress",
		"--newline",
		"--print-traffic",
		"-v",
		"-o", getPath(track, ".%(ext)s"),
	}

	if track.Video {
		args = append(
			args,
			"-f",
			"(b[height>=360][height<=1080]/bv*[height>=360][height<=1080]/bv*)+(ba[abr>=180][abr<=360]/ba)/b",
		)
	} else {
		args = append(args,
			"-f", "ba[abr>=180][abr<=360]/ba",
			"-x",
		)
	}

	if y.isYouTubeURL(track.URL) {
		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil && cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
		}
	}

	args = append(args, track.URL)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(&stdoutBuf, stdoutPipe)
	}()

	go func() {
		defer wg.Done()
		io.Copy(&stderrBuf, stderrPipe)
	}()

	err := cmd.Wait()
	wg.Wait()

	outStr := stdoutBuf.String()
	errStr := stderrBuf.String()

	if err != nil {

		gologging.Error("YtDlp Download Failed")
		gologging.ErrorF("URL: %s", track.URL)
		gologging.ErrorF("Error: %v", err)

		if outStr != "" {
			gologging.Error("----- YTDLP STDOUT -----")
			logLarge("", outStr)
		}

		if errStr != "" {
			gologging.Error("----- YTDLP STDERR -----")
			logLarge("", errStr)
		}

		findAndRemove(track)

		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}

		return "", fmt.Errorf("yt-dlp error: %w", err)
	}

	path := findFile(track)
	if path == "" {
		return "", errors.New("yt-dlp did not return output file path")
	}

	gologging.InfoF("YtDlp: Successfully downloaded %s", path)

	return path, nil
}

func (*YtdlpPlatform) CanSearch() bool { return false }

func (*YtdlpPlatform) Search(string, bool) ([]*state.Track, error) {
	return nil, nil
}

func (y *YtdlpPlatform) extractMetadata(urlStr string) (*ytdlpInfo, error) {

	args := []string{
		"-j",
		"--flat-playlist",
		"--no-warnings",
		"--no-check-certificate",
	}

	if y.isYouTubeURL(urlStr) {
		cookieFile, err := cookies.GetRandomCookieFile()
		if err == nil && cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
		}
	}

	args = append(args, urlStr)

	cmd := exec.Command("yt-dlp", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		gologging.ErrorF("YtDlp Metadata Error: %v", err)
		logLarge("", stderr.String())
		return nil, fmt.Errorf("metadata extraction failed: %w", err)
	}

	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) > 1 {

		var info ytdlpInfo
		info.Entries = make([]ytdlpInfo, 0, len(lines))

		for _, line := range lines {

			var entry ytdlpInfo

			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}

			info.Entries = append(info.Entries, entry)
		}

		if len(info.Entries) == 0 {
			return nil, errors.New("no valid entries found")
		}

		return &info, nil
	}

	var info ytdlpInfo

	if err := json.Unmarshal([]byte(output), &info); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &info, nil
}

func (y *YtdlpPlatform) infoToTrack(info *ytdlpInfo, video bool) *state.Track {

	duration := int(info.Duration)

	trackURL := info.URL

	if info.OriginalURL != "" {
		trackURL = info.OriginalURL
	}

	return &state.Track{
		ID:       info.ID,
		Title:    info.Title,
		Duration: duration,
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

func (y *YtdlpPlatform) CanGetRecommendations() bool {
	return false
}

func (y *YtdlpPlatform) GetRecommendations(track *state.Track) ([]*state.Track, error) {
	return nil, errors.New("recommendations not supported")
}
