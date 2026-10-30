package service

import (
	"strings"

	"star-meet-backend/core/emotion"
)

const defaultEmotionIntensity = 1

func evolveCharacterEmotion(previous string, previousIntensity int, proposed string, userMessage string, behavior string) (string, int) {
	previous = strings.TrimSpace(previous)
	proposed = strings.TrimSpace(proposed)
	if previous == "" {
		previous = "平静"
	}
	if previousIntensity < 1 {
		previousIntensity = defaultEmotionIntensity
	}
	if previousIntensity > 5 {
		previousIntensity = 5
	}
	if proposed == "" {
		proposed = inferCharacterEmotion(userMessage, behavior)
	}

	userEmotion := emotion.Detect(userMessage)
	if isCalmCharacterEmotion(proposed) && !isCalmCharacterEmotion(previous) && isLowIntensityUserEmotion(userEmotion) {
		if previousIntensity > 1 {
			return previous, previousIntensity - 1
		}
		return "平静", 1
	}
	if proposed == previous {
		if previousIntensity < 5 && userEmotion != emotion.EmotionNeutral {
			previousIntensity++
		}
		return proposed, previousIntensity
	}

	intensity := 2
	if userEmotion != emotion.EmotionNeutral {
		intensity = 3
	}
	return proposed, intensity
}

func isCalmCharacterEmotion(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "平静", "平靜", "calm", "neutral":
		return true
	default:
		return false
	}
}

func inferCharacterEmotion(userMessage, behavior string) string {
	switch behavior {
	case BehaviorActionComfort, BehaviorActionRepair:
		return "担心"
	case BehaviorActionPlayful:
		return "开心"
	case BehaviorActionVulnerable:
		return "心动"
	case BehaviorActionPause:
		return "克制"
	}
	if emotion.Detect(userMessage) == emotion.EmotionHappy {
		return "开心"
	}
	return "平静"
}

func isLowIntensityUserEmotion(value string) bool {
	switch value {
	case "", emotion.EmotionNeutral, emotion.EmotionBored:
		return true
	default:
		return false
	}
}
