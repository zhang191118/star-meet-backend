package emotion

import "testing"

func TestTraditionalAndNegatedEmotionPhrases(t *testing.T) {
	for _, test := range []struct{ text, want string }{
		{"我今天不開心", EmotionSad},
		{"I feel unhappy", EmotionSad},
		{"我不喜歡喝奶茶", EmotionNeutral},
		{"我沒有想你", EmotionNeutral},
		{"我不擔心", EmotionNeutral},
		{"我很難過", EmotionSad},
		{"我很開心", EmotionHappy},
		{"我並不寂寞", EmotionNeutral},
	} {
		if got := Detect(test.text); got != test.want {
			t.Errorf("%q: got %s, want %s", test.text, got, test.want)
		}
	}
}
func TestEmotionTiesAreDeterministic(t *testing.T) {
	first := Detect("我有點委屈，也很想你")
	for i := 0; i < 50; i++ {
		if got := Detect("我有點委屈，也很想你"); got != first {
			t.Fatalf("nondeterministic emotion: %s vs %s", got, first)
		}
	}
}
