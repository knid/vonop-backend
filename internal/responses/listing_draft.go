package responses

// ListingDraftResponse carries a single drafted field. The Pilot
// calls the endpoint once per field (title / description /
// release_notes) and substitutes the result into the in-progress form.
type ListingDraftResponse struct {
	Kind  string `json:"kind"`  // echo of the request
	Value string `json:"value"` // the drafted copy
}
