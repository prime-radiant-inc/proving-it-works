package narrate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	ttsModel  = "gpt-4o-mini-tts" // reads exactly what it is sent
	chatModel = "gpt-audio-1.5"   // better prosody, will ad-lib; gated
)

type openAI struct {
	key  string
	chat bool
}

func (o openAI) Name() string {
	if o.chat {
		return "openai-chat"
	}
	return "openai"
}

func (o openAI) Model(string) string {
	if o.chat {
		return chatModel
	}
	return ttsModel
}

func (openAI) DefaultVoice() string { return "nova" }

func (openAI) Ready(string) error { return nil }

func (o openAI) Synthesize(text, voice, wav string) (string, error) {
	if !o.chat {
		audio, err := o.post("https://api.openai.com/v1/audio/speech", map[string]any{
			"model": ttsModel, "voice": voice, "input": text, "response_format": "wav"})
		if err != nil {
			return "", err
		}
		return "", os.WriteFile(wav, audio, 0o644)
	}
	body, err := o.post("https://api.openai.com/v1/chat/completions", map[string]any{
		"model":      chatModel,
		"modalities": []string{"text", "audio"},
		"audio":      map[string]string{"voice": voice, "format": "wav"},
		"messages": []map[string]string{{"role": "user", "content": "Read this narration aloud, " +
			"warm and clear, verbatim, and say nothing else:\n\n" + text}},
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data       string `json:"data"`
					Transcript string `json:"transcript"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("openai-chat response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("openai-chat returned no choices")
	}
	a := resp.Choices[0].Message.Audio
	audio, err := base64.StdEncoding.DecodeString(a.Data)
	if err != nil {
		return "", fmt.Errorf("openai-chat audio: %w", err)
	}
	return a.Transcript, os.WriteFile(wav, audio, 0o644)
}

func (o openAI) post(url string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.key)
	req.Header.Set("Content-Type", "application/json")
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
		return nil, fmt.Errorf("%s: %s: %.300s", url, resp.Status, out)
	}
	return out, nil
}
