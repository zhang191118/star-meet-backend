package service

import (
	"strings"

	"star-meet-backend/core/emotion"
)

const (
	BehaviorActionAcknowledge = "acknowledge"
	BehaviorActionComfort     = "comfort"
	BehaviorActionExplain     = "explain"
	BehaviorActionAsk         = "ask"
	BehaviorActionPlayful     = "playful"
	BehaviorActionInvite      = "invite"
	BehaviorActionVulnerable  = "vulnerable"
	BehaviorActionRepair      = "repair"
	BehaviorActionPause       = "pause"
)

var behaviorActions = map[string]struct{}{
	BehaviorActionAcknowledge: {}, BehaviorActionComfort: {}, BehaviorActionExplain: {},
	BehaviorActionAsk: {}, BehaviorActionPlayful: {}, BehaviorActionInvite: {},
	BehaviorActionVulnerable: {}, BehaviorActionRepair: {}, BehaviorActionPause: {},
}

func normalizeBehaviorAction(action, userEmotion, characterEmotion, plotState string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	if _, ok := behaviorActions[action]; ok {
		return action
	}

	// The fallback is deliberately small and deterministic. It gives the
	// generator a useful intent even when the planner returns malformed JSON.
	switch userEmotion {
	case emotion.EmotionSad, emotion.EmotionLonely, emotion.EmotionFear:
		return BehaviorActionComfort
	case emotion.EmotionAngry:
		if plotState == PlotStateTension {
			return BehaviorActionRepair
		}
		return BehaviorActionAcknowledge
	case emotion.EmotionConfused:
		return BehaviorActionExplain
	case emotion.EmotionLove:
		return BehaviorActionVulnerable
	case emotion.EmotionHappy, emotion.EmotionExcited:
		return BehaviorActionPlayful
	}
	if strings.Contains(characterEmotion, "疲惫") || strings.Contains(characterEmotion, "克制") {
		return BehaviorActionPause
	}
	if plotState == PlotStateOpening {
		return BehaviorActionAsk
	}
	return BehaviorActionAcknowledge
}

func behaviorActionHint(action string) string {
	switch action {
	case BehaviorActionComfort:
		return "先接住用户情绪，再给出陪伴或具体支持，不急着说教。"
	case BehaviorActionRepair:
		return "承认关系中的紧张，解释或道歉，并给用户继续表达的空间。"
	case BehaviorActionVulnerable:
		return "适度透露角色自己的感受，保持克制，不把情感变成压力。"
	case BehaviorActionPlayful:
		return "分享用户的好情绪，可以调侃或活泼互动，但不要抢走话题。"
	case BehaviorActionInvite:
		return "提出一个自然、可拒绝的下一步互动，不强迫用户接受。"
	case BehaviorActionExplain:
		return "先直接回答用户真正困惑的部分，再用简短补充说明。"
	case BehaviorActionAsk:
		return "只问一个有上下文的问题，让用户容易接着聊。"
	case BehaviorActionPause:
		return "语气放慢，少说一点，给对话留下呼吸感。"
	default:
		return "先回应用户刚才表达的重点，再自然推进对话。"
	}
}
