package controllers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/knid/wemob/internal/constants"
	"github.com/slack-go/slack"
)

type KadinGirisimcilerFile struct {
	File     multipart.File
	FileName string
}

type KadinGirisimcilerSubmission struct {
	IsimSoyisim    string
	Telefon        string
	EPosta         string
	VergiNo        string
	TicaretSicilNo string
	IsTanimi       string

	GirisimciBelgesi *KadinGirisimcilerFile
	FaaliyetBelgesi  *KadinGirisimcilerFile
}

func (c *Controller) SubmitKadinGirisimciler(sub KadinGirisimcilerSubmission) error {
	msg := fmt.Sprintf(
		"*Yeni Kadin Girisimciler Basvurusu*\n\n"+
			"*Isim Soyisim:* %s\n"+
			"*Telefon:* %s\n"+
			"*E-posta:* %s\n"+
			"*Vergi No:* %s\n"+
			"*Ticaret/Esnaf Sicil No:* %s\n"+
			"*Is Tanimi:* %s",
		sub.IsimSoyisim, sub.Telefon, sub.EPosta, sub.VergiNo, sub.TicaretSicilNo, sub.IsTanimi,
	)

	_, _, err := c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_KADIN_GIRISIMCILER,
		slack.MsgOptionText(msg, false),
	)
	if err != nil {
		return fmt.Errorf("slack message: %w", err)
	}

	if err := uploadFileToSlack(c, sub.GirisimciBelgesi, fmt.Sprintf("Girisimci Belgesi - %s", sub.IsimSoyisim)); err != nil {
		return err
	}
	if err := uploadFileToSlack(c, sub.FaaliyetBelgesi, fmt.Sprintf("Faaliyet Belgesi - %s", sub.IsimSoyisim)); err != nil {
		return err
	}

	return nil
}

func uploadFileToSlack(c *Controller, f *KadinGirisimcilerFile, title string) error {
	if f == nil || f.File == nil {
		return nil
	}

	fileBytes, err := io.ReadAll(f.File)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	_, err = c.SlackService.Client.UploadFileV2(slack.UploadFileV2Parameters{
		Channel:  constants.SLACK_CHANNEL_ID_KADIN_GIRISIMCILER,
		FileSize: len(fileBytes),
		Reader:   io.NopCloser(bytes.NewReader(fileBytes)),
		Filename: f.FileName,
		Title:    title,
	})
	if err != nil {
		return fmt.Errorf("slack file upload: %w", err)
	}

	return nil
}
