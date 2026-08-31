package service

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestLegacyPriceTierSelectorMatchesAfterNormalization(t *testing.T) {
	legacySelector, err := json.Marshal(map[string]string{
		"operation":  "TEXT_TO_VIDEO",
		"resolution": "720",
		"duration":   "6",
	})
	if err != nil {
		t.Fatal(err)
	}

	intent := ModelRequestIntent{
		Capability: "video",
		Operation:  "text_to_video",
		Options:    map[string]any{"vquality": "720p", "videoSeconds": "6"},
	}
	channelModel := model.ChannelModel{PriceTiers: []model.ChannelModelPriceTier{{
		SelectorJSON: string(legacySelector), Enabled: true, PriceConfigured: true,
	}}}
	if matched := channelModelPriceTierForIntent(channelModel, intent); matched == nil {
		t.Fatal("legacy selector should match normalized request")
	}
}

func TestLegacyPriceTierMetadataCompletesPartialSelector(t *testing.T) {
	selector, err := json.Marshal(map[string]string{"operation": "text_to_video"})
	if err != nil {
		t.Fatal(err)
	}

	intent := ModelRequestIntent{
		Capability: "video",
		Operation:  "text_to_video",
		Options:    map[string]any{"vquality": "1080P", "videoSeconds": 8},
	}
	channelModel := model.ChannelModel{PriceTiers: []model.ChannelModelPriceTier{{
		SelectorJSON: string(selector), Resolution: "1080", VideoSeconds: 8, Enabled: true, PriceConfigured: true,
	}}}
	if matched := channelModelPriceTierForIntent(channelModel, intent); matched == nil {
		t.Fatal("partial legacy selector should use tier metadata")
	}
}

func TestPriceTierStillRejectsUnmatchedResolution(t *testing.T) {
	selector, err := json.Marshal(map[string]string{"vquality": "720p"})
	if err != nil {
		t.Fatal(err)
	}

	intent := ModelRequestIntent{Capability: "video", Options: map[string]any{"vquality": "1080p"}}
	channelModel := model.ChannelModel{PriceTiers: []model.ChannelModelPriceTier{{
		SelectorJSON: string(selector), Enabled: true, PriceConfigured: true,
	}}}
	if matched := channelModelPriceTierForIntent(channelModel, intent); matched != nil {
		t.Fatalf("unmatched resolution selected tier: %#v", matched)
	}
}
