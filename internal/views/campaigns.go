package views

import (
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) SubmitKadinGirisimciler(w http.ResponseWriter, r *http.Request) {
	// 20 MB max (multiple file uploads)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		utils.JSONError(w, constants.ErrAssetTooLarge, "File too large or invalid form data.", http.StatusBadRequest, nil)
		return
	}

	isimSoyisim := r.FormValue("isim_soyisim")
	telefon := r.FormValue("telefon")
	ePosta := r.FormValue("e_posta")
	vergiNo := r.FormValue("vergi_no")
	ticaretSicilNo := r.FormValue("ticaret_sicil_no")
	isTanimi := r.FormValue("is_tanimi")

	if isimSoyisim == "" || telefon == "" || ePosta == "" || vergiNo == "" || isTanimi == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "isim_soyisim, telefon, e_posta, vergi_no, and is_tanimi are required.", http.StatusBadRequest, map[string]any{
			"fields": []string{"isim_soyisim", "telefon", "e_posta", "vergi_no", "is_tanimi"},
		})
		return
	}

	sub := controllers.KadinGirisimcilerSubmission{
		IsimSoyisim:    isimSoyisim,
		Telefon:        telefon,
		EPosta:         ePosta,
		VergiNo:        vergiNo,
		TicaretSicilNo: ticaretSicilNo,
		IsTanimi:       isTanimi,
	}

	if file, header, err := r.FormFile("girisimci_belgesi"); err == nil {
		defer file.Close()
		sub.GirisimciBelgesi = &controllers.KadinGirisimcilerFile{File: file, FileName: header.Filename}
	}

	if file, header, err := r.FormFile("faaliyet_belgesi"); err == nil {
		defer file.Close()
		sub.FaaliyetBelgesi = &controllers.KadinGirisimcilerFile{File: file, FileName: header.Filename}
	}

	if err := v.Controller.SubmitKadinGirisimciler(sub); err != nil {
		slog.Error("kadin girisimciler submission failed", "error", err)
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusCreated)
}
