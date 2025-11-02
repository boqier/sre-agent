package main
import (
	"context"
	"errors"

	"github.com/sashabaranov/go-openai"
)

type OpenAI struct {
	Client *openai.Client
	ctx    context.Context
}

func NewOpenAIClient() (*OpenAI, error) {
	apiKey := "sk-lqcuebxcbfrtrwlckktalpvvsnwxomdneswvuhytfqoookrw"
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://api.siliconflow.cn/v1"
	client := openai.NewClientWithConfig(config)

	ctx := context.Background()

	return &OpenAI{
		Client: client,
		ctx:    ctx,
	}, nil
}

func (o *OpenAI) SendMessage(prompt string, content string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: "Qwen/Qwen2.5-72B-Instruct",
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    "system",
				Content: prompt,
			},
			{
				Role:    "user",
				Content: content,
			},
		},
	}

	resp, err := o.Client.CreateChatCompletion(o.ctx, req)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", errors.New("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}
