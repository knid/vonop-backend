package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) runDeterministicAutoFixes(
	project *models.AppProject,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	appliedOps *[]appBuilderOperation,
	filesChanged *int,
) {
	// Auto-fix .ts files containing JSX → rename to .tsx
	tsxRenamed, tsxChangedPaths := autoFixTSFilesWithJSX(fileMap)
	if tsxRenamed > 0 {
		slog.Info("Auto-fix: renamed .ts→.tsx", "project_id", project.ID, "count", tsxRenamed)
		for i := 0; i+1 < len(tsxChangedPaths); i += 2 {
			oldPath := tsxChangedPaths[i]
			newPath := tsxChangedPaths[i+1]
			_, fileType := guessFileType(newPath)

			// Delete old .ts file and write new .tsx file in repo
			repo.DeleteFile(oldPath)
			if writeErr := repo.WriteFile(newPath, []byte(fileMap[newPath])); writeErr != nil {
				slog.Error("failed to write renamed file", "error", writeErr, "old", oldPath, "new", newPath)
			} else {
				// Emit delete for old path so sandbox removes the stale .ts file
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "delete", FilePath: oldPath,
					FileType: "ts",
				})
				sendEvent("file_complete", responses.FileCompleteEvent{
					Action: "create", FilePath: newPath,
					FileType: fileType, Content: fileMap[newPath],
				})
			}
			*filesChanged++
		}
	}

	// Auto-fix metadata in client component
	autoFixedMeta, changedPathsMeta := autoFixMetadataInClientComponent(fileMap)

	// Auto-fix font weight for non-variable Google Fonts
	autoFixedFonts, changedPathsFonts := autoFixFontWeights(fileMap)

	// Auto-fix import/export mismatches
	autoFixed, changedPaths := autoFixImportMismatches(fileMap)
	autoFixed += autoFixedFonts
	changedPaths = append(changedPaths, changedPathsFonts...)
	// Auto-fix missing "use client" directives
	autoFixed2, changedPaths2 := autoFixMissingUseClient(fileMap)
	autoFixed += autoFixed2
	changedPaths = append(changedPaths, changedPaths2...)
	autoFixed += autoFixedMeta
	changedPaths = append(changedPaths, changedPathsMeta...)

	// Auto-fix root layout tags
	if fixed, fixedPath := autoFixRootLayoutTags(fileMap); fixed && fixedPath != "" {
		changedPaths = append(changedPaths, fixedPath)
		autoFixed++
	}

	// Auto-fix className string concatenation → template literals
	autoFixedConcat, changedPathsConcat := autoFixClassNameConcat(fileMap)
	autoFixed += autoFixedConcat
	changedPaths = append(changedPaths, changedPathsConcat...)

	// Auto-fix unterminated JSX string expressions {"text → {"text"}
	autoFixedJSXStr, changedPathsJSXStr := autoFixUnterminatedJSXStrings(fileMap)
	autoFixed += autoFixedJSXStr
	changedPaths = append(changedPaths, changedPathsJSXStr...)

	// Auto-fix missing force-dynamic on DB-backed pages
	autoFixedDynamic, changedPathsDynamic := autoFixMissingForceDynamic(fileMap)
	autoFixed += autoFixedDynamic
	changedPaths = append(changedPaths, changedPathsDynamic...)

	if autoFixed > 0 {
		// Collect fix types for the announcement
		var fixTypes []string
		if tsxRenamed > 0 {
			fixTypes = append(fixTypes, "ts_to_tsx")
		}
		if autoFixedMeta > 0 {
			fixTypes = append(fixTypes, "metadata_client_component")
		}
		if autoFixedFonts > 0 {
			fixTypes = append(fixTypes, "font_weights")
		}
		if autoFixed2 > 0 {
			fixTypes = append(fixTypes, "use_client")
		}
		if autoFixedConcat > 0 {
			fixTypes = append(fixTypes, "classname_concat")
		}
		if autoFixedJSXStr > 0 {
			fixTypes = append(fixTypes, "jsx_strings")
		}

		// Announce auto-fixes to the frontend
		sendEvent("progress", responses.ProgressEvent{
			Phase:   "auto_fix",
			Message: fmt.Sprintf("Polishing %d file(s)...", autoFixed+tsxRenamed),
		})

		slog.Info("Deterministic auto-fix applied", "project_id", project.ID, "files_fixed", autoFixed)
		for _, fixedPath := range changedPaths {
			_, fileType := guessFileType(fixedPath)
			action := "modify"
			if !repo.FileExists(fixedPath) {
				action = "create"
			}
			if writeErr := repo.WriteFile(fixedPath, []byte(fileMap[fixedPath])); writeErr != nil {
				slog.Error("runDeterministicAutoFixes: failed to persist auto-fix", "path", fixedPath, "error", writeErr)
				continue
			}
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action: action, FilePath: fixedPath,
				FileType: fileType,
				Content: fileMap[fixedPath],
			})
			*appliedOps = append(*appliedOps, appBuilderOperation{
				Action: action, Path: fixedPath, Type: fileType,
			})
			*filesChanged++
		}

		// Emit auto-fix summary event
		sendEvent("auto_fix", responses.AutoFixEvent{
			FilesFixed: autoFixed + tsxRenamed,
			FixTypes:   fixTypes,
			Paths:      changedPaths,
		})
	}
}

// --- Tool definitions ---

// filterToolsByName extracts tools from appBuilderTools matching the given names.

// generateMissingPlannedFiles checks if any files from the structure plan were not
// generated (e.g. due to token limit) and generates them in a follow-up LLM call.
func (c *Controller) generateMissingPlannedFiles(
	ctx context.Context,
	structure *appFileStructure,
	project *models.AppProject,
	model string,
	provider *utils.LLMProvider,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
	mobileConfig *models.MobileAppConfig,
	originalMessage string,
) {
	var missingFiles []appFilePlanEntry
	for _, f := range structure.Files {
		if _, exists := fileMap[f.Path]; !exists {
			// Also check if the file was created at a slightly different path
			// (e.g. .ts vs .tsx rename by auto-fix)
			altPath := f.Path
			if strings.HasSuffix(altPath, ".ts") && !strings.HasSuffix(altPath, ".d.ts") {
				altPath = strings.TrimSuffix(altPath, ".ts") + ".tsx"
			}
			if _, altExists := fileMap[altPath]; !altExists {
				missingFiles = append(missingFiles, f)
			}
		}
	}

	if len(missingFiles) == 0 {
		return
	}

	slog.Info("Generating missing planned files",
		"project_id", project.ID,
		"missing_count", len(missingFiles),
		"planned_total", len(structure.Files),
	)

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "generating_files",
		Message: fmt.Sprintf("Completing %d remaining file(s)...", len(missingFiles)),
	})

	// Build a focused prompt listing just the missing files + existing file summaries
	var missingList strings.Builder
	for _, f := range missingFiles {
		missingList.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.Path, f.Type, f.Description))
	}

	var existingList strings.Builder
	for path, content := range fileMap {
		summary := extractFileSummary(content)
		if summary != "" {
			existingList.WriteString(fmt.Sprintf("- %s: %s\n", path, summary))
		} else {
			existingList.WriteString(fmt.Sprintf("- %s\n", path))
		}
	}

	recoveryPrompt := fmt.Sprintf(`The initial generation was cut short. The following files from the plan were NOT generated yet:

%s
Files already generated (for import reference):
%s
User's original request: %s

Generate ONLY the missing files listed above. Each file must be complete, production-ready code.
Import from existing files as needed — check the existing file list above for correct paths and exports.
Follow the same design system, color scheme, and coding patterns as the existing files.`, missingList.String(), existingList.String(), originalMessage)

	recoveryMessages := []utils.Message{
		{Role: "system", Content: buildSingleShotSystemPrompt(project, mobileConfig)},
		{Role: "system", Content: c.buildProjectContext(project, fileMap, true)},
		{Role: "user", Content: recoveryPrompt},
	}

	recoveryParser := &utils.FileStreamParser{
		DiscardIncomplete: true,
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			if saveErr := c.saveGeneratedFile(project, repo, path, content, fileType, fileMap, sendEvent, ops); saveErr != nil {
				slog.Error("recovery file save failed", "path", path, "error", saveErr)
			}
		},
		OnText: func(text string) {
			// Discard recovery text — don't stream it to user
		},
	}

	fixModel := c.fixModel(provider)
	_, recoveryErr := utils.StreamTextFromLLMWithMessages(
		ctx, fixModel, recoveryMessages, provider,
		func(chunk string) { recoveryParser.Feed(chunk) },
		nil, // discard thinking output
		utils.StreamTextConfig{
			MaxTokens: 32768,
			Reasoning: &utils.ReasoningConfig{Effort: "low"},
		},
	)
	recoveryParser.Flush()

	if recoveryErr != nil {
		slog.Warn("Missing file recovery pass failed", "error", recoveryErr, "project_id", project.ID)
	} else {
		// Count how many were recovered
		recovered := 0
		for _, f := range missingFiles {
			if _, exists := fileMap[f.Path]; exists {
				recovered++
			}
		}
		slog.Info("Missing file recovery completed",
			"project_id", project.ID,
			"recovered", recovered,
			"still_missing", len(missingFiles)-recovered,
		)
	}
}

