package views

import (
	"fmt"
	"log"
	"net/http"

	"github.com/knid/wemob/internal/utils"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/billingportal/session"
)

func (v *HttpViews) StripeCustomerPortal(w http.ResponseWriter, r *http.Request) {
	/* 	tokenString, err := utils.ExtractTokenFromHeader(r)
	   	if err != nil {
	   		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
	   		return
	   	} */

	tokenString := r.PostFormValue("token")
	fmt.Println("Token:", tokenString)

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	params := stripe.BillingPortalSessionParams{
		Configuration: stripe.String("bpc_1SEIoRGdh8XYrKRBeWgGwDLe"),
		Customer:      stripe.String(user.CustomerID),
	}

	s, err := session.New(&params)
	if err != nil {
		log.Printf("session.New: %v", err) // Log the detailed error
		http.Error(w, "Failed to create checkout session. Please try again.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}
