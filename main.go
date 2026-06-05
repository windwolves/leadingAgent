package main

import (
	"fmt"
	"log"
	"os"

	"leadingAgent/config"
	"leadingAgent/models/deepseek"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekAPIURL, cfg.DeepSeekModel)

	userInput := "Hello, how are you?"
	if len(os.Args) > 1 {
		userInput = os.Args[1]
	}

	messages := []deepseek.Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: userInput,
		},
	}

	resp, err := client.Chat(messages, nil)
	if err != nil {
		log.Fatalf("Failed to call DeepSeek API: %v", err)
	}

	if len(resp.Choices) > 0 {
		fmt.Println("Response:", resp.Choices[0].Message.Content)
	}
}