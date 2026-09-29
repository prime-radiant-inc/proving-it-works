package narrate

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prime-radiant-inc/proving-it-works/internal/ffmpeg"
)

const (
	pollyEngine     = "generative" // the most natural voices; not every voice or region has it
	pollySampleRate = "24000"      // mp3 only: Polly serves PCM at 16 kHz at most
	pollyMaxChars   = 3000         // SynthesizeSpeech's limit on billed characters per request
)

// polly is Amazon Polly's generative engine. The voice comes from the scenes
// file, the region from AWS_REGION, and the credentials from the standard
// AWS environment variables.
type polly struct {
	creds    awsCreds
	region   string
	endpoint string // empty: https://polly.<region>.amazonaws.com
}

type awsCreds struct{ id, secret, token string }

func newPolly() (polly, error) {
	creds := awsCreds{
		id:     strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
		secret: strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
		token:  strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN")),
	}
	if creds.id == "" || creds.secret == "" {
		return polly{}, errors.New("engine polly needs AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY " +
			"for a user allowed polly:DescribeVoices and polly:SynthesizeSpeech")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	return polly{creds: creds, region: region}, nil
}

func (polly) Name() string         { return "polly" }
func (polly) Model(string) string  { return pollyEngine }
func (polly) DefaultVoice() string { return "Ruth" }
func (p polly) url(path string) string {
	if p.endpoint != "" {
		return p.endpoint + path
	}
	return "https://polly." + p.region + ".amazonaws.com" + path
}

// Ready checks the voice exists for the generative engine in this region:
// availability differs by region, and a clear error here beats a failure on
// the first scene.
func (p polly) Ready(voice string) error {
	var ids []string
	next := ""
	for {
		q := url.Values{"Engine": {pollyEngine}}
		if next != "" {
			q.Set("NextToken", next)
		}
		body, err := p.do(http.MethodGet, "/v1/voices?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		var page struct {
			Voices    []struct{ Id string }
			NextToken string
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("polly voices: %w", err)
		}
		for _, v := range page.Voices {
			ids = append(ids, v.Id)
		}
		if next = page.NextToken; next == "" {
			break
		}
	}
	if slices.Contains(ids, voice) {
		return nil
	}
	sort.Strings(ids)
	return fmt.Errorf("polly voice %s is not available with the %s engine in %s; "+
		"set AWS_REGION to a region that has it, or pick one of: %s",
		voice, pollyEngine, p.region, strings.Join(ids, ", "))
}

// Synthesize asks for MP3 and converts it, because Polly's PCM stops at
// 16 kHz. The MP3 is kept next to the WAV: it is what was paid for, and the
// generative voices change as AWS updates them, so asking again later need
// not give back the same take.
func (p polly) Synthesize(text, voice, wav string) (string, error) {
	if n := utf8.RuneCountInString(text); n > pollyMaxChars {
		return "", fmt.Errorf("polly reads at most %d characters per clip; this one has %d: split the scene",
			pollyMaxChars, n)
	}
	body, err := json.Marshal(map[string]string{
		"Engine": pollyEngine, "OutputFormat": "mp3", "SampleRate": pollySampleRate,
		"Text": text, "TextType": "text", "VoiceId": voice})
	if err != nil {
		return "", err
	}
	audio, err := p.do(http.MethodPost, "/v1/speech", body)
	if err != nil {
		return "", err
	}
	mp3 := strings.TrimSuffix(wav, ".wav") + ".mp3"
	if err := os.WriteFile(mp3, audio, 0o644); err != nil {
		return "", err
	}
	return "", ffmpeg.Run(filepath.Dir(wav), nil, "-i", mp3, "-ac", "1", "-c:a", "pcm_s16le", wav)
}

func (p polly) do(method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(method, p.url(path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	signV4(req, body, p.creds, p.region, "polly", time.Now())
	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("polly %s %s: %s: %.300s", method, path, resp.Status, out)
	}
	return out, nil
}

// signV4 signs req with AWS Signature Version 4, covering the host, the
// date, the session token when there is one, and the content type when set.
func signV4(req *http.Request, body []byte, c awsCreds, region, service string, now time.Time) {
	stamp := now.UTC().Format("20060102T150405Z")
	day := stamp[:8]
	req.Header.Set("X-Amz-Date", stamp)
	if c.token != "" {
		req.Header.Set("X-Amz-Security-Token", c.token)
	}
	headers := map[string]string{"host": req.URL.Host, "x-amz-date": stamp}
	if c.token != "" {
		headers["x-amz-security-token"] = c.token
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		headers["content-type"] = ct
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signed := strings.Join(names, ";")

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	// AWS wants %20 for a space, where Go's query encoding writes +.
	query := strings.ReplaceAll(req.URL.Query().Encode(), "+", "%20")
	payload := sha256.Sum256(body)
	canonical := strings.Join([]string{req.Method, path, query, canonHeaders.String(), signed,
		hex.EncodeToString(payload[:])}, "\n")

	scope := day + "/" + region + "/" + service + "/aws4_request"
	hashed := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(hashed[:])

	key := []byte("AWS4" + c.secret)
	for _, part := range []string{day, region, service, "aws4_request"} {
		key = hmacSHA256(key, part)
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.id+"/"+scope+
		", SignedHeaders="+signed+", Signature="+hex.EncodeToString(hmacSHA256(key, toSign)))
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
