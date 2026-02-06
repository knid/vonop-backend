package tasks

const TypePublishWebsite = "publish:website"

type PublishWebsitePayload struct {
	WebsiteID string
	UserID    string
}
