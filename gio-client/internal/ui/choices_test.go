package ui

import (
	"testing"

	"github.com/yuanhua/image-gptcodex/pkg/client"
)

func TestVisibleResolutionChoicesDefaultsBlankImageModelToGPTImage2(t *testing.T) {
	choices := visibleResolutionChoices(string(client.APIModeResponses), string(client.RequestPolicyOpenAI), "")
	if len(choices) != len(resolutionChoices) {
		t.Fatalf("len(choices)=%d want %d", len(choices), len(resolutionChoices))
	}
	want := map[string]bool{
		"auto": true,
		"1k":   true,
		"2k":   true,
		"4k":   true,
	}
	for _, item := range choices {
		if !want[item.Value] {
			t.Fatalf("unexpected resolution choice %q", item.Value)
		}
		delete(want, item.Value)
	}
	if len(want) != 0 {
		t.Fatalf("missing resolution choices: %#v", want)
	}
}
