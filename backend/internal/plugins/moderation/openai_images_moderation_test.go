package moderation

// 本文件自 internal/service/openai_images_test.go 随迁（Phase-6 搬家）：
// 这两个用例测试 OpenAI images 请求解析（service.OpenAIImagesRequest.ModerationBody，
// 留在 service 包）与内容审计输入提取（本包 ExtractContentModerationInput）的集成，
// 依赖本包未导出成员（buildLog / defaultContentModerationConfig），故归属本包。

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

func TestOpenAIImagesRequestModerationBody_JSONEditIncludesInputImageURLs(t *testing.T) {
	parsed := &service.OpenAIImagesRequest{
		Endpoint:       "/v1/images/edits",
		Prompt:         "replace background",
		InputImageURLs: []string{"https://example.com/source.png"},
		MaskImageURL:   "https://example.com/mask.png",
	}

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, parsed.ModerationBody())

	require.Equal(t, "replace background", input.Text)
	require.Equal(t, []string{"https://example.com/source.png", "https://example.com/mask.png"}, input.Images)
}

func TestOpenAIImagesRequestModerationBody_MultipartEditIncludesUploadsInMemory(t *testing.T) {
	parsed := &service.OpenAIImagesRequest{
		Endpoint: "/v1/images/edits",
		Prompt:   "replace background",
		Uploads: []service.OpenAIImagesUpload{{
			FieldName:   "image",
			FileName:    "source.png",
			ContentType: "image/png",
			Data:        []byte("fake-image-bytes"),
		}},
		MaskUpload: &service.OpenAIImagesUpload{
			FieldName:   "mask",
			FileName:    "mask.png",
			ContentType: "image/png",
			Data:        []byte("fake-mask-bytes"),
		},
	}

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, parsed.ModerationBody())

	require.Equal(t, "replace background", input.Text)
	require.Equal(t, []string{
		"data:image/png;base64,ZmFrZS1pbWFnZS1ieXRlcw==",
		"data:image/png;base64,ZmFrZS1tYXNrLWJ5dGVz",
	}, input.Images)

	log := (&ContentModerationService{}).buildLog(ContentModerationCheckInput{}, defaultContentModerationConfig(), ContentModerationActionAllow, false, "", 0, nil, input.ExcerptText(), nil, nil, "")
	require.Equal(t, "replace background", log.InputExcerpt)
	require.NotContains(t, log.InputExcerpt, "ZmFrZS")
}
