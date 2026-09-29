package narrate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The two vanilla cases of AWS's Signature Version 4 test suite: if these
// signatures match, the canonical request, string to sign, and key
// derivation are all right.
func TestSignV4MatchesTheAWSTestSuite(t *testing.T) {
	creds := awsCreds{id: "AKIDEXAMPLE", secret: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	at := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	for _, c := range []struct{ method, want string }{
		{http.MethodGet, "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"},
		{http.MethodPost, "5da7c1a2acd57cee7505fc6676e4e544621c30862966e37dddb68e92efbe5d6b"},
	} {
		req, _ := http.NewRequest(c.method, "https://example.amazonaws.com/", nil)
		signV4(req, nil, creds, "us-east-1", "service", at)
		want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
			"SignedHeaders=host;x-amz-date, Signature=" + c.want
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.method, got, want)
		}
	}
}

func TestSignV4SignsTheSessionToken(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	signV4(req, nil, awsCreds{id: "a", secret: "b", token: "tok"}, "us-east-1", "polly", time.Now())
	if req.Header.Get("X-Amz-Security-Token") != "tok" ||
		!strings.Contains(req.Header.Get("Authorization"), "SignedHeaders=host;x-amz-date;x-amz-security-token") {
		t.Fatalf("session token not signed: %v", req.Header)
	}
}

func setAWSEnv(t *testing.T, id, secret, region, defaultRegion string) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", id)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", region)
	t.Setenv("AWS_DEFAULT_REGION", defaultRegion)
}

func TestPollyWithoutCredentialsIsAnError(t *testing.T) {
	setAWSEnv(t, "", "", "", "")
	if _, err := Resolve("polly"); err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("got %v, want an error naming AWS_ACCESS_KEY_ID", err)
	}
}

func TestPollyRegionAndVoiceDefaults(t *testing.T) {
	for _, c := range []struct{ region, defaultRegion, want string }{
		{"", "", "us-east-1"},
		{"", "eu-west-1", "eu-west-1"},
		{"eu-central-1", "eu-west-1", "eu-central-1"},
	} {
		setAWSEnv(t, "id", "secret", c.region, c.defaultRegion)
		e, err := Resolve("polly")
		if err != nil {
			t.Fatal(err)
		}
		p := e.(polly)
		if p.region != c.want || e.Name() != "polly" || e.DefaultVoice() != "Ruth" {
			t.Errorf("AWS_REGION=%q AWS_DEFAULT_REGION=%q: region %q voice %q, want %q Ruth",
				c.region, c.defaultRegion, p.region, e.DefaultVoice(), c.want)
		}
	}
}

// fakePolly answers DescribeVoices with Ruth alone and SynthesizeSpeech with
// mp3, recording the last synthesis request.
func fakePolly(t *testing.T, mp3 []byte, got *map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/voices":
			if r.URL.Query().Get("Engine") != "generative" {
				t.Errorf("DescribeVoices query %q", r.URL.RawQuery)
			}
			w.Write([]byte(`{"Voices":[{"Id":"Ruth","LanguageCode":"en-US","SupportedEngines":["generative"]}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/speech":
			json.NewDecoder(r.Body).Decode(got)
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write(mp3)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testPolly(endpoint string) polly {
	return polly{creds: awsCreds{id: "id", secret: "secret"}, region: "eu-central-1", endpoint: endpoint}
}

func TestPollyReadyNamesTheRegionWhenTheVoiceIsMissing(t *testing.T) {
	p := testPolly(fakePolly(t, nil, nil).URL)
	if err := p.Ready("Ruth"); err != nil {
		t.Fatalf("Ruth: %v", err)
	}
	err := p.Ready("Sergio")
	if err == nil || !strings.Contains(err.Error(), "Sergio") || !strings.Contains(err.Error(), "eu-central-1") {
		t.Fatalf("got %v, want an error naming the voice and the region", err)
	}
}

func TestPollyKeepsTheMP3AndWritesAWAV(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	mp3 := filepath.Join(dir, "tone.mp3")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=d=0.5",
		"-ar", "24000", mp3).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	audio, _ := os.ReadFile(mp3)
	var got map[string]string
	p := testPolly(fakePolly(t, audio, &got).URL)

	wav := filepath.Join(dir, "clip.wav")
	if _, err := p.Synthesize("Hello there.", "Ruth", wav); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Engine": "generative", "OutputFormat": "mp3", "SampleRate": "24000",
		"Text": "Hello there.", "TextType": "text", "VoiceId": "Ruth"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("request %s = %q, want %q", k, got[k], v)
		}
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, "clip.mp3")); string(kept) != string(audio) {
		t.Error("the MP3 Polly returned was not kept byte for byte")
	}
	if head, _ := os.ReadFile(wav); len(head) < 4 || string(head[:4]) != "RIFF" {
		t.Error("no WAV written")
	}
}

func TestPollyRefusesTextOverItsLimitWithoutARequest(t *testing.T) {
	p := testPolly("http://127.0.0.1:1") // nothing listens: a request would fail differently
	_, err := p.Synthesize(strings.Repeat("a", pollyMaxChars+1), "Ruth", filepath.Join(t.TempDir(), "x.wav"))
	if err == nil || !strings.Contains(err.Error(), "3000") {
		t.Fatalf("got %v, want the character limit", err)
	}
}

// TestPollyLive synthesizes one clip against the real service. It runs only
// with AWS credentials in the environment, and costs a fraction of a cent.
func TestPollyLive(t *testing.T) {
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		t.Skip("needs AWS credentials")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("needs ffmpeg")
	}
	e, err := Resolve("polly")
	if err != nil {
		t.Fatal(err)
	}
	voice := os.Getenv("POLLY_TEST_VOICE")
	if voice == "" {
		voice = e.DefaultVoice()
	}
	if err := e.Ready(voice); err != nil {
		t.Fatal(err)
	}
	wav, _, err := Clip(t.TempDir(), e, voice, "This clip was read by Amazon Polly.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSuffix(wav, ".wav") + ".mp3"); err != nil {
		t.Fatalf("the MP3 was not kept next to %s", wav)
	}
}
