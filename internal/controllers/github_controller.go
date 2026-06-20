package controllers

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// ConnectGitHub exchanges an OAuth code for a token and saves it on the user.
func (c *Controller) ConnectGitHub(user *models.User, code string) error {
	clientID := c.SecretManager.GetSecret("GITHUB_CLIENT_ID")
	clientSecret := c.SecretManager.GetSecret("GITHUB_CLIENT_SECRET")
	redirectURI := c.SecretManager.GetSecret("GITHUB_REDIRECT_URI")
	encryptionKey := c.SecretManager.GetSecret("GITHUB_TOKEN_ENCRYPTION_KEY")

	tokenResp, err := utils.ExchangeGitHubCode(clientID, clientSecret, code, redirectURI)
	if err != nil {
		return fmt.Errorf("failed to exchange GitHub code: %w", err)
	}

	ghUser, err := utils.FetchGitHubUser(tokenResp.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to fetch GitHub user: %w", err)
	}

	encrypted, err := utils.EncryptToken(tokenResp.AccessToken, encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt GitHub token: %w", err)
	}

	user.GitHubAccessToken = encrypted
	user.GitHubUsername = ghUser.Login

	if err := c.DB.UpdateUserGitHubFields(user); err != nil {
		return fmt.Errorf("failed to save GitHub credentials: %w", err)
	}

	slog.Info("GitHub connected", "user", user.Email, "github_username", ghUser.Login)
	return nil
}

// DisconnectGitHub removes the user's GitHub token and clears all export records.
func (c *Controller) DisconnectGitHub(user *models.User) error {
	user.GitHubAccessToken = ""
	user.GitHubUsername = ""
	if err := c.DB.UpdateUserGitHubFields(user); err != nil {
		return fmt.Errorf("failed to clear GitHub credentials: %w", err)
	}

	exports, err := c.DB.GetAppGitHubExportsByUserID(user.ID.String())
	if err == nil {
		for _, export := range exports {
			c.DB.DeleteAppGitHubExport(export.AppProjectID.String())
		}
	}

	slog.Info("GitHub disconnected", "user", user.Email)
	return nil
}

// decryptGitHubToken is a helper to decrypt the user's stored GitHub token.
func (c *Controller) decryptGitHubToken(user *models.User) (string, error) {
	if user.GitHubAccessToken == "" {
		return "", fmt.Errorf("GitHub not connected")
	}
	encryptionKey := c.SecretManager.GetSecret("GITHUB_TOKEN_ENCRYPTION_KEY")
	return utils.DecryptToken(user.GitHubAccessToken, encryptionKey)
}

// ExportToGitHub creates a new GitHub repo and pushes the project's full git history.
func (c *Controller) ExportToGitHub(user *models.User, projectID uuid.UUID, req requests.ExportToGitHubRequest) (*models.AppGitHubExport, error) {
	token, err := c.decryptGitHubToken(user)
	if err != nil {
		return nil, err
	}

	existing, _ := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if existing != nil && existing.ID != uuid.Nil {
		return nil, fmt.Errorf("project already exported to GitHub as %s — disconnect first to re-export", existing.RepoFullName)
	}

	private := req.PrivateRepo()
	repoResp, err := utils.CreateGitHubRepo(token, req.RepoName, req.Description, private)
	if err != nil {
		if err.Error() == "GITHUB_REPO_EXISTS" {
			return nil, fmt.Errorf("GITHUB_REPO_EXISTS")
		}
		return nil, fmt.Errorf("failed to create GitHub repo: %w", err)
	}

	repo, err := c.GitRepoService.Open(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo: %w", err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		branch = "main"
	}

	pushURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, repoResp.FullName)
	if err := repo.PushToURL(pushURL, branch); err != nil {
		return nil, fmt.Errorf("failed to push to GitHub: %w", err)
	}

	export := &models.AppGitHubExport{
		AppProjectID: projectID,
		UserID:       user.ID,
		RepoFullName: repoResp.FullName,
		RepoURL:      repoResp.HTMLURL,
		Private:      private,
	}
	if _, err := c.DB.CreateAppGitHubExport(export); err != nil {
		return nil, fmt.Errorf("failed to save export record: %w", err)
	}

	c.DB.UpdateAppGitHubExportLastPushed(projectID.String())

	slog.Info("GitHub export completed", "user", user.Email, "repo", repoResp.FullName)
	return export, nil
}

// PushToGitHub pushes the latest changes to an already-exported GitHub repo.
func (c *Controller) PushToGitHub(user *models.User, projectID uuid.UUID) (*models.AppGitHubExport, error) {
	token, err := c.decryptGitHubToken(user)
	if err != nil {
		return nil, err
	}

	export, err := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("project not exported to GitHub — export first")
	}
	if export.UserID != user.ID {
		return nil, fmt.Errorf("not authorized")
	}

	repo, err := c.GitRepoService.Open(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo: %w", err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		branch = "main"
	}

	pushURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, export.RepoFullName)
	if err := repo.PushToURL(pushURL, branch); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "could not read Username") || strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") {
			return nil, fmt.Errorf("GITHUB_AUTH_EXPIRED")
		}
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
			c.DB.DeleteAppGitHubExport(projectID.String())
			return nil, fmt.Errorf("GITHUB_REPO_NOT_FOUND")
		}
		if strings.Contains(errMsg, "non-fast-forward") || strings.Contains(errMsg, "rejected") {
			return nil, fmt.Errorf("GITHUB_PUSH_REJECTED")
		}
		if strings.Contains(errMsg, "signal: killed") || strings.Contains(errMsg, "context deadline exceeded") {
			return nil, fmt.Errorf("GITHUB_PUSH_TIMEOUT")
		}
		return nil, fmt.Errorf("failed to push to GitHub: %w", err)
	}

	c.DB.UpdateAppGitHubExportLastPushed(projectID.String())

	export, _ = c.DB.GetAppGitHubExportByProjectID(projectID.String())
	return export, nil
}

// PullFromGitHub fast-forwards the project repo from its exported GitHub repo.
func (c *Controller) PullFromGitHub(user *models.User, projectID uuid.UUID) (*models.AppGitHubExport, error) {
	token, err := c.decryptGitHubToken(user)
	if err != nil {
		return nil, err
	}

	export, err := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("project not exported to GitHub — export first")
	}
	if export.UserID != user.ID {
		return nil, fmt.Errorf("not authorized")
	}

	repo, err := c.GitRepoService.Open(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo: %w", err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		branch = "main"
	}

	pullURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, export.RepoFullName)
	if err := repo.PullFromURL(pullURL, branch); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "could not read Username") || strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") {
			return nil, fmt.Errorf("GITHUB_AUTH_EXPIRED")
		}
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
			c.DB.DeleteAppGitHubExport(projectID.String())
			return nil, fmt.Errorf("GITHUB_REPO_NOT_FOUND")
		}
		if strings.Contains(errMsg, "Not possible to fast-forward") ||
			strings.Contains(errMsg, "not possible to fast-forward") ||
			strings.Contains(errMsg, "divergent") ||
			strings.Contains(errMsg, "non-fast-forward") ||
			strings.Contains(errMsg, "would be overwritten") {
			return nil, fmt.Errorf("GITHUB_PULL_CONFLICT")
		}
		if strings.Contains(errMsg, "signal: killed") || strings.Contains(errMsg, "context deadline exceeded") {
			return nil, fmt.Errorf("GITHUB_PULL_TIMEOUT")
		}
		return nil, fmt.Errorf("failed to pull from GitHub: %w", err)
	}

	export, _ = c.DB.GetAppGitHubExportByProjectID(projectID.String())
	return export, nil
}

// RemoveGitHubExport deletes the export record for a project.
func (c *Controller) RemoveGitHubExport(user *models.User, projectID uuid.UUID) error {
	export, err := c.DB.GetAppGitHubExportByProjectID(projectID.String())
	if err != nil {
		return fmt.Errorf("no GitHub export found for this project")
	}
	if export.UserID != user.ID {
		return fmt.Errorf("not authorized")
	}
	return c.DB.DeleteAppGitHubExport(projectID.String())
}

// GetGitHubExport returns the export record for a project.
func (c *Controller) GetGitHubExport(projectID uuid.UUID) (*models.AppGitHubExport, error) {
	return c.DB.GetAppGitHubExportByProjectID(projectID.String())
}

// GetUserById returns a user by their ID string.
func (c *Controller) GetUserById(id string) (*models.User, error) {
	return c.DB.GetUserById(id)
}
