package textnorm

import (
	"strings"
	"sync"

	"github.com/liuzl/gocc"
)

const (
	ChineseScriptSimplified  = "zh-Hans"
	ChineseScriptTraditional = "zh-Hant"
	ChineseScriptOriginal    = "original"
)

var (
	t2sOnce sync.Once
	t2sConv *gocc.OpenCC
	t2sErr  error
)

// NormalizeChineseScript applies the requested Chinese script policy.
// The empty policy defaults to Simplified Chinese because most AgenDash voice
// flows expect mainland Chinese UI and TTS text.
func NormalizeChineseScript(text, script string) string {
	switch normalizeChineseScript(script) {
	case ChineseScriptOriginal, ChineseScriptTraditional:
		return text
	default:
		out, err := TraditionalToSimplified(text)
		if err != nil {
			return text
		}
		return out
	}
}

func TraditionalToSimplified(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	t2sOnce.Do(func() {
		t2sConv, t2sErr = gocc.New("t2s")
	})
	if t2sErr != nil {
		return text, t2sErr
	}
	return t2sConv.Convert(text)
}

func normalizeChineseScript(script string) string {
	switch strings.ToLower(strings.TrimSpace(script)) {
	case "", "auto", "zh", "zh-cn", "zh-sg", "zh-hans", "zh-hans-cn", "simplified", "simplified-chinese", "简体", "简体中文", "簡體", "簡體中文":
		return ChineseScriptSimplified
	case "zh-hant", "zh-hant-tw", "zh-tw", "zh-hk", "zh-mo", "traditional", "traditional-chinese", "繁体", "繁體", "繁体中文", "繁體中文":
		return ChineseScriptTraditional
	case "none", "off", "disable", "disabled", "raw", "source", "original", "keep":
		return ChineseScriptOriginal
	default:
		return ChineseScriptSimplified
	}
}
