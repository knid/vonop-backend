package constants

// FlyRegion describes one Fly Machines region exposed to users via the
// region-picker UI on Pro+ plans. The canonical list lives at
// https://fly.io/docs/reference/regions/ — keep this in sync when Fly adds
// or deprecates a region.
type FlyRegion struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

// FlyRegions is the curated set of regions we expose for app deployment.
// Pinned to Fly's documented "all regions" list (gateway-or-not), since
// shared-cpu-1x — the only machine size we ship — is available in every
// listed region.
var FlyRegions = []FlyRegion{
	{Code: "ams", Name: "Amsterdam", Country: "Netherlands"},
	{Code: "arn", Name: "Stockholm", Country: "Sweden"},
	{Code: "bom", Name: "Mumbai", Country: "India"},
	{Code: "cdg", Name: "Paris", Country: "France"},
	{Code: "dfw", Name: "Dallas", Country: "United States"},
	{Code: "ewr", Name: "Secaucus", Country: "United States"},
	{Code: "fra", Name: "Frankfurt", Country: "Germany"},
	{Code: "gru", Name: "São Paulo", Country: "Brazil"},
	{Code: "iad", Name: "Ashburn", Country: "United States"},
	{Code: "jnb", Name: "Johannesburg", Country: "South Africa"},
	{Code: "lax", Name: "Los Angeles", Country: "United States"},
	{Code: "lhr", Name: "London", Country: "United Kingdom"},
	{Code: "nrt", Name: "Tokyo", Country: "Japan"},
	{Code: "ord", Name: "Chicago", Country: "United States"},
	{Code: "sin", Name: "Singapore", Country: "Singapore"},
	{Code: "sjc", Name: "San Jose", Country: "United States"},
	{Code: "syd", Name: "Sydney", Country: "Australia"},
	{Code: "yyz", Name: "Toronto", Country: "Canada"},
}
