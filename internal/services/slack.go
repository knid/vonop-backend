package services

import "github.com/slack-go/slack"

type SlackService struct {
	Client slack.Client
}

func NewSlackService(token string) *SlackService {
	return &SlackService{
		Client: *slack.New(token),
	}
}
