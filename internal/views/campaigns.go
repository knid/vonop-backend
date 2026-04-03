package views

import (
	"log/slog"
	"net/http"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) SubmitKadinGirisimciler(w http.ResponseWriter, r *http.Request) {
	// 10 MB max
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.JSONErrorResponse(w, "file too large or invalid form data", http.StatusBadRequest)
		return
	}

	isimSoyisim := r.FormValue("isim_soyisim")
	dogumTarihi := r.FormValue("dogum_tarihi")
	kullanimAmaci := r.FormValue("kullanim_amaci")

	if isimSoyisim == "" || dogumTarihi == "" || kullanimAmaci == "" {
		utils.JSONErrorResponse(w, "isim_soyisim, dogum_tarihi, and kullanim_amaci are required", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("vergi_levhasi")
	if err != nil {
		utils.JSONErrorResponse(w, "vergi_levhasi file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	sub := controllers.KadinGirisimcilerSubmission{
		IsimSoyisim:   isimSoyisim,
		DogumTarihi:   dogumTarihi,
		KullanimAmaci: kullanimAmaci,
		File:          file,
		FileName:      header.Filename,
	}

	if err := v.Controller.SubmitKadinGirisimciler(sub); err != nil {
		slog.Error("kadin girisimciler submission failed", "error", err)
		utils.JSONErrorResponse(w, "failed to submit application", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusCreated)
}
