package controllers

import (
	"reflect"
	"sort"
	"testing"

	"github.com/knid/wemob/internal/responses"
)

func TestParseRoutes_BasicGET(t *testing.T) {
	files := map[string]string{
		"backend/app/api/todos/route.ts": "export async function GET() { return Response.json([]) }",
	}
	got := ParseRoutes(files)
	want := []RouteEntry{{Method: "GET", Path: "/api/todos", File: "backend/app/api/todos/route.ts"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseRoutes_MultipleMethodsInOneFile(t *testing.T) {
	files := map[string]string{
		"backend/app/api/todos/route.ts": `
            export async function GET() {}
            export async function POST() {}
            export function DELETE() {}
        `,
	}
	got := ParseRoutes(files)
	sort.Slice(got, func(i, j int) bool { return got[i].Method < got[j].Method })
	want := []RouteEntry{
		{Method: "DELETE", Path: "/api/todos", File: "backend/app/api/todos/route.ts"},
		{Method: "GET", Path: "/api/todos", File: "backend/app/api/todos/route.ts"},
		{Method: "POST", Path: "/api/todos", File: "backend/app/api/todos/route.ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseRoutes_DynamicSegment(t *testing.T) {
	files := map[string]string{
		"backend/app/api/users/[id]/route.ts": "export async function GET() {}",
	}
	got := ParseRoutes(files)
	if len(got) != 1 || got[0].Path != "/api/users/:id" {
		t.Errorf("unexpected path: %+v", got)
	}
}

func TestParseRoutes_CatchAllSegment(t *testing.T) {
	files := map[string]string{
		"backend/app/api/files/[...slug]/route.ts": "export async function GET() {}",
	}
	got := ParseRoutes(files)
	if len(got) != 1 || got[0].Path != "/api/files/*slug" {
		t.Errorf("unexpected path: %+v", got)
	}
}

func TestParseRoutes_NestedDirectories(t *testing.T) {
	files := map[string]string{
		"backend/app/api/users/[id]/posts/[postId]/route.ts": "export async function GET() {}",
	}
	got := ParseRoutes(files)
	if len(got) != 1 || got[0].Path != "/api/users/:id/posts/:postId" {
		t.Errorf("unexpected path: %+v", got)
	}
}

func TestParseRoutes_IgnoresNonRouteFiles(t *testing.T) {
	files := map[string]string{
		"backend/app/api/todos/route.ts":        "export async function GET() {}",
		"backend/app/api/todos/helper.ts":       "export function noop() {}",
		"backend/app/api/todos/route.ts.backup": "export async function POST() {}",
		"mobile/app/(tabs)/index.tsx":           "export default function Screen() {}",
	}
	got := ParseRoutes(files)
	if len(got) != 1 || got[0].Method != "GET" {
		t.Errorf("expected only one GET, got %+v", got)
	}
}

func TestParseRoutes_NoHandlersIsEmpty(t *testing.T) {
	files := map[string]string{
		"backend/app/api/todos/route.ts": "// TODO add a handler",
	}
	got := ParseRoutes(files)
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestDeriveBackendColor(t *testing.T) {
	tests := []struct {
		name string
		in   responses.BackendHealthResponse
		want string
	}{
		{"no backend", responses.BackendHealthResponse{HasBackend: false}, "gray"},
		{"never deployed", responses.BackendHealthResponse{HasBackend: true, MachineState: ""}, "yellow"},
		{"deploy failed", responses.BackendHealthResponse{HasBackend: true, MachineState: "started", LastDeployAt: "t", LastDeployOK: false}, "red"},
		{"machine destroyed", responses.BackendHealthResponse{HasBackend: true, MachineState: "destroyed", LastDeployAt: "t", LastDeployOK: true}, "red"},
		{"started + deployed", responses.BackendHealthResponse{HasBackend: true, MachineState: "started", LastDeployAt: "t", LastDeployOK: true}, "green"},
		{"auto-stopped (idle)", responses.BackendHealthResponse{HasBackend: true, MachineState: "stopped", LastDeployAt: "t", LastDeployOK: true}, "yellow"},
		{"deploying", responses.BackendHealthResponse{HasBackend: true, MachineState: "started", IsDeploying: true, DeployStatus: "building"}, "yellow"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveBackendColor(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
