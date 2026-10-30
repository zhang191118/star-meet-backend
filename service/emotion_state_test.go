package service

import "testing"

func TestEvolveCharacterEmotionKeepsInertiaBeforeReturningToCalm(t *testing.T) {
	emotion, intensity := evolveCharacterEmotion("生气", 3, "平静", "嗯嗯", BehaviorActionAcknowledge)
	if emotion != "生气" || intensity != 2 {
		t.Fatalf("expected emotional inertia to decay one step, got %q/%d", emotion, intensity)
	}
	emotion, intensity = evolveCharacterEmotion(emotion, intensity, "平静", "嗯嗯", BehaviorActionAcknowledge)
	if emotion != "生气" || intensity != 1 {
		t.Fatalf("expected second neutral turn to keep a trace of emotion, got %q/%d", emotion, intensity)
	}
	emotion, intensity = evolveCharacterEmotion(emotion, intensity, "平静", "嗯嗯", BehaviorActionAcknowledge)
	if emotion != "平静" || intensity != 1 {
		t.Fatalf("expected emotion to return to calm after decay, got %q/%d", emotion, intensity)
	}
}

func TestEvolveCharacterEmotionLetsStrongUserSignalChangeState(t *testing.T) {
	emotion, intensity := evolveCharacterEmotion("平静", 1, "担心", "我有点害怕", BehaviorActionComfort)
	if emotion != "担心" || intensity < 2 {
		t.Fatalf("expected strong user signal to move character emotion, got %q/%d", emotion, intensity)
	}
}
