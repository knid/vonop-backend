//go:build integration

// Package eval contains integration tests for the AI agent against
// known prompts. Build-tag gated so they don't run in standard `go test ./...`.
//
// To run: go test -tags=integration ./internal/agent/eval/ -v -timeout 10m
// (Requires ANTHROPIC_API_KEY; cost ~$0.50 per full run.)
package eval

// Fixture is one prompt + the structural assertions on the generated code.
type Fixture struct {
	Name           string
	Prompt         string
	MustContain    []string // substrings the combined generated code MUST include
	MustNotContain []string // substrings the combined generated code MUST NOT include
}

// ExpoFixtures covers ~10 representative mobile app prompts.
var ExpoFixtures = []Fixture{
	{
		Name:   "habit_tracker",
		Prompt: "Build a habit tracker mobile app where users sign up, log daily habits, and see streaks.",
		MustContain: []string{
			`from "expo-router"`,
			`from "@/lib/api"`,
			`className=`,
			`Stack`,
		},
		MustNotContain: []string{`from "next/`, `<div`, `<span`, `<button`, `style={{`},
	},
	{
		Name:   "recipe_app",
		Prompt: "Mobile app for sharing recipes with photos and ratings.",
		MustContain:    []string{`from "expo-router"`, `from "@/lib/api"`, `className=`},
		MustNotContain: []string{`from "next/`, `<div`},
	},
	{
		Name:   "fitness_tracker",
		Prompt: "Native iOS fitness tracker with workout logs and weekly progress.",
		MustContain:    []string{`from "expo-router"`, `className=`},
		MustNotContain: []string{`from "next/`, `<div`, `<button`},
	},
	{
		Name:   "chat_app",
		Prompt: "Mobile chat app with rooms, message history, and push notifications.",
		MustContain:    []string{`from "expo-router"`, `className=`, `apiFetch`},
		MustNotContain: []string{`from "next/`, `<div`},
	},
	{
		Name:   "photo_gallery",
		Prompt: "Photo gallery mobile app where users can upload and browse images.",
		MustContain:    []string{`from "expo-router"`, `className=`},
		MustNotContain: []string{`from "next/`, `<img`},
	},
	{
		Name:   "weather",
		Prompt: "Weather mobile app showing current conditions and 5-day forecast.",
		MustContain:    []string{`from "expo-router"`, `className=`},
		MustNotContain: []string{`from "next/`, `<div`},
	},
	{
		Name:   "news_reader",
		Prompt: "News reader mobile app with categories and saved articles.",
		MustContain:    []string{`from "expo-router"`, `className=`},
		MustNotContain: []string{`from "next/`, `<div`, `<a `},
	},
	{
		Name:   "journal",
		Prompt: "Personal journaling mobile app with biometric login.",
		MustContain:    []string{`from "expo-router"`, `className=`},
		MustNotContain: []string{`from "next/`, `<div`},
	},
	{
		Name:   "todo",
		Prompt: "Simple to-do app on Expo with sign-up and sync.",
		MustContain:    []string{`from "expo-router"`, `className=`, `apiFetch`},
		MustNotContain: []string{`from "next/`, `<div`},
	},
	{
		Name:   "ecommerce_mobile",
		Prompt: "Mobile shop app for handmade jewelry with a polished product browsing flow.",
		MustContain:    []string{`from "expo-router"`, `className=`},
		MustNotContain: []string{`from "next/`, `Stripe.checkout`},
	},
}
