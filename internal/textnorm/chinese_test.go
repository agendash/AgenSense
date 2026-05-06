package textnorm

import "testing"

func TestTraditionalToSimplified(t *testing.T) {
	t.Parallel()

	got, err := TraditionalToSimplified("語音測試，請啟動服務並回覆。")
	if err != nil {
		t.Fatalf("TraditionalToSimplified() error = %v", err)
	}
	want := "语音测试，请启动服务并回复。"
	if got != want {
		t.Fatalf("TraditionalToSimplified() = %q, want %q", got, want)
	}
}

func TestNormalizeChineseScriptOriginal(t *testing.T) {
	t.Parallel()

	in := "語音測試"
	if got := NormalizeChineseScript(in, "original"); got != in {
		t.Fatalf("NormalizeChineseScript(original) = %q, want %q", got, in)
	}
}
