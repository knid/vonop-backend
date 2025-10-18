package requests

type CreateTicketRequest struct {
	Subject     string `json:"subject" bun:",notnull"`
	Message     string `json:"message" bun:",notnull"`
	SenderName  string `json:"sender_name"`
	SenderEmail string `json:"sender_email"`
}
