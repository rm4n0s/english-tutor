package llmtest

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/tmc/langchaingo/llms"
)

func ask(f *Fake, prompt string) (string, error) {
	resp, err := f.GenerateContent(context.Background(), []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, prompt),
	})
	if err != nil {
		return "", err
	}
	return resp.Choices[0].Content, nil
}

func TestRepliesPlayInOrderAndLastRepeats(t *testing.T) {
	boom := errors.New("Inner", "boom")
	f := New(Reply{Text: "one"}, Reply{Err: boom}, Reply{Text: "three"})
	tests := []struct {
		want    string
		wantErr error
	}{
		{want: "one"},
		{wantErr: boom},
		{want: "three"},
		{want: "three"},
	}
	for i, tt := range tests {
		got, err := ask(f, "q")
		if !errors.Is(err, tt.wantErr) || got != tt.want {
			t.Errorf("call %d = %q, %v; want %q, %v", i+1, got, err, tt.want, tt.wantErr)
		}
	}
	if n := len(f.Calls()); n != 4 {
		t.Errorf("recorded calls = %d, want 4", n)
	}
}

func TestCallWorksThroughTheSimpleInterface(t *testing.T) {
	got, err := New(Reply{Text: "ok"}).Call(context.Background(), "q")
	if err != nil || got != "ok" {
		t.Errorf("Call() = %q, %v", got, err)
	}
}
