package tts

import (
	"context"
	"testing"
)

func TestSynthesize(t *testing.T) {
	// 这是一个简单的冒烟测试，验证是否能从微软服务器获取到音频数据
	// 注意：由于依赖网络，如果网络不通或 IP 被封禁可能会失败
	ctx := context.Background()
	audio, err := Synthesize(ctx, "这是一段用于测试语音合成的文本。协议更新后应该可以正常工作。", "zh-CN-YunyangNeural", "+0%", "+0%")
	
	if err != nil {
		t.Fatalf("语音合成失败: %v", err)
	}
	
	if len(audio) == 0 {
		t.Fatal("返回的音频数据长度为 0")
	}
	
	t.Logf("成功合成语音，数据长度: %d 字节", len(audio))
}
