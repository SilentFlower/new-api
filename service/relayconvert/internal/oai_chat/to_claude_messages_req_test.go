package oaichat

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaymedia "github.com/QuantumNous/new-api/service/relayconvert/internal/media"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesConvertsTextFileToTextBlock(t *testing.T) {
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(_ *gin.Context, _ types.FileSource, _ ...string) (string, string, error) {
			return base64.StdEncoding.EncodeToString([]byte("文件内容")), "text/plain", nil
		},
	})
	t.Cleanup(func() {
		relaymedia.SetMediaResolver(relaymedia.MediaResolver{})
	})

	message := dto.Message{Role: "user"}
	message.SetMediaContent([]dto.MediaContent{
		{
			Type: dto.ContentTypeFile,
			File: &dto.MessageFile{
				FileName: "notes.txt",
				FileData: "placeholder",
			},
		},
	})

	request, err := OpenAIChatRequestToClaudeMessages(nil, dto.GeneralOpenAIRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []dto.Message{message},
	})

	require.NoError(t, err)
	require.Len(t, request.Messages, 1)
	content, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "text", content[0].Type)
	require.NotNil(t, content[0].Text)
	assert.Equal(t, "文件内容", *content[0].Text)
}
