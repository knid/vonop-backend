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

// runValidationFixPass runs deterministic checks on the generated files and, if
// errors are found, asks the fast model to fix them in one pass. This catches
// import mismatches, missing "use client", broken links, wrong export types, etc.
// that the auto-fixes couldn't resolve deterministically.
func (c *Controller) runValidationFixPass(
	ctx context.Context,
	project *models.AppProject,
	provider *utils.LLMProvider,
	repo *gitrepo.Repo,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
	mobileConfig *models.MobileAppConfig,
) {
	errors := runDeterministicChecks(fileMap)
	if len(errors) == 0 {
		return
	}

	slog.Info("Validation fix pass: detected errors",
		"project_id", project.ID,
		"error_count", len(errors),
	)

	sendEvent("progress", responses.ProgressEvent{
		Phase:   "auto_fix",
		Message: fmt.Sprintf("Fixing %d detected issue(s)...", len(errors)),
	})

	// Build error summary for the LLM
	var errorSummary strings.Builder
	// Group errors by file for clarity
	errorsByFile := make(map[string][]validationError)
	for _, e := range errors {
		errorsByFile[e.File] = append(errorsByFile[e.File], e)
	}
	for file, fileErrors := range errorsByFile {
		errorSummary.WriteString(fmt.Sprintf("\n### %s\n", file))
		for _, e := range fileErrors {
			errorSummary.WriteString(fmt.Sprintf("- [%s] %s\n", e.Type, e.Error))
		}
	}

	// Include the content of errored files so the LLM can fix them.
	// Cap included files to prevent massive inputs that cause output truncation.
	const maxIncludedFiles = 15
	const maxIncludedChars = 60000
	var fileContents strings.Builder
	includedFiles := make(map[string]bool)
	totalChars := 0

	for file := range errorsByFile {
		if len(includedFiles) >= maxIncludedFiles || totalChars >= maxIncludedChars {
			break
		}
		if content, ok := fileMap[file]; ok && !includedFiles[file] {
			includedFiles[file] = true
			entry := fmt.Sprintf("\n--- %s ---\n%s\n", file, content)
			fileContents.WriteString(entry)
			totalChars += len(entry)
		}
		// Also include files referenced in import errors
		for _, e := range errorsByFile[file] {
			if len(includedFiles) >= maxIncludedFiles || totalChars >= maxIncludedChars {
				break
			}
			if e.Type == "missing_import" || e.Type == "import_mismatch" || e.Type == "invalid_named_import" {
				// Extract the target file path from the error message
				for targetPath, targetContent := range fileMap {
					if len(includedFiles) >= maxIncludedFiles || totalChars >= maxIncludedChars {
						break
					}
					if strings.Contains(e.Error, targetPath) && !includedFiles[targetPath] {
						includedFiles[targetPath] = true
						entry := fmt.Sprintf("\n--- %s ---\n%s\n", targetPath, targetContent)
						fileContents.WriteString(entry)
						totalChars += len(entry)
					}
				}
			}
		}
	}

	fixPrompt := fmt.Sprintf(`The following errors were detected in the generated code. Fix ALL of them.
%s
### Current file contents:
%s
Output ONLY the fixed files using <file path="...">complete content</file> blocks. Output complete file content for each fixed file.
Do NOT change design, layout, or functionality — ONLY fix the reported errors.`, errorSummary.String(), fileContents.String())

	fixMessages := []utils.Message{
		{Role: "system", Content: buildChatSystemPrompt(project, mobileConfig)},
		{Role: "user", Content: fixPrompt},
	}

	fixParser := &utils.FileStreamParser{
		DiscardIncomplete: true,
		OnFile: func(path, content string) {
			content = utils.StripCodeFences(content)
			_, fileType := guessFileType(path)
			if fileType == "tsx" || fileType == "ts" {
				content, _ = stripForbiddenImports(content, path)
			}
			if saveErr := c.saveGeneratedFile(project, repo, path, content, fileType, fileMap, sendEvent, ops, true); saveErr != nil {
				slog.Error("fix pass file save failed", "path", path, "error", saveErr)
			}
		},
		OnText: func(text string) {
			// Discard fix pass text — don't stream to user
		},
	}

	fixModel := c.fixModel(provider)
	_, fixErr := utils.StreamTextFromLLMWithMessages(
		ctx, fixModel, fixMessages, provider,
		func(chunk string) { fixParser.Feed(chunk) },
		nil,
		utils.StreamTextConfig{MaxTokens: 65536},
	)
	fixParser.Flush()

	if fixErr != nil {
		slog.Warn("Validation fix pass LLM call failed", "error", fixErr, "project_id", project.ID)
	} else {
		// Re-run deterministic auto-fixes on the fixed files
		fixFilesChanged := 0
		c.runDeterministicAutoFixes(project, repo, fileMap, sendEvent, ops, &fixFilesChanged)

		remainingErrors := runDeterministicChecks(fileMap)
		slog.Info("Validation fix pass completed",
			"project_id", project.ID,
			"original_errors", len(errors),
			"remaining_errors", len(remainingErrors),
		)
	}
}
