package controllers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/knid/wemob/internal/constants"
	"github.com/slack-go/slack"
)

type KadinGirisimcilerSubmission struct {
	IsimSoyisim   string
	Telefon       string
	DogumTarihi   string
	KullanimAmaci string
	File          multipart.File
	FileName      string
}

func (c *Controller) SubmitKadinGirisimciler(sub KadinGirisimcilerSubmission) error {
	msg := fmt.Sprintf(
		"*Yeni Kadin Girisimciler Basvurusu*\n\n"+
			"*Isim Soyisim:* %s\n"+
			"*Telefon:* %s\n"+
			"*Dogum Tarihi:* %s\n"+
			"*Kullanim Amaci:* %s",
		sub.IsimSoyisim, sub.Telefon, sub.DogumTarihi, sub.KullanimAmaci,
	)

	// Send the text message first
	_, _, err := c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_KADIN_GIRISIMCILER,
		slack.MsgOptionText(msg, false),
	)
	if err != nil {
		return fmt.Errorf("slack message: %w", err)
	}

	// Upload the vergi levhasi file to the same channel
	if sub.File != nil {
		fileBytes, err := io.ReadAll(sub.File)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}

		_, err = c.SlackService.Client.UploadFileV2(slack.UploadFileV2Parameters{
			Channel:  constants.SLACK_CHANNEL_ID_KADIN_GIRISIMCILER,
			FileSize: len(fileBytes),
			Reader:   io.NopCloser(bytes.NewReader(fileBytes)),
			Filename: sub.FileName,
			Title:    fmt.Sprintf("Vergi Levhasi - %s", sub.IsimSoyisim),
		})
		if err != nil {
			return fmt.Errorf("slack file upload: %w", err)
		}
	}

	return nil
}
