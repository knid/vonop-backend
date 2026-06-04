package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetMCPConnectionsByProjectID(projectID string) ([]models.MCPConnection, error) {
	var connections []models.MCPConnection
	err := db.DB.NewSelect().
		Model(&connections).
		Where("app_project_id = ?", projectID).
		Where("deleted_at IS NULL").
		OrderExpr("created_at ASC").
		Scan(context.Background())
	return connections, err
}

func (db *Database) GetMCPConnectionByID(id string) (*models.MCPConnection, error) {
	var connection models.MCPConnection
	err := db.DB.NewSelect().
		Model(&connection).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Scan(context.Background())
	return &connection, err
}

func (db *Database) CreateMCPConnection(connection *models.MCPConnection) (*models.MCPConnection, error) {
	_, err := db.DB.NewInsert().Model(connection).Exec(context.Background())
	return connection, err
}

func (db *Database) UpdateMCPConnection(connection *models.MCPConnection) (*models.MCPConnection, error) {
	connection.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(connection).WherePK().Exec(context.Background())
	return connection, err
}

func (db *Database) DeleteMCPConnection(connection *models.MCPConnection) error {
	if _, err := db.DB.NewUpdate().
		Model((*models.MCPConnection)(nil)).
		Set("credential_data = '{}'::jsonb").
		Set("config = '{}'::jsonb").
		Set("credentials_encrypted_v1 = FALSE").
		Set("last_error = ''").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", connection.ID).
		Exec(context.Background()); err != nil {
		return err
	}
	_, err := db.DB.NewDelete().Model(connection).WherePK().Exec(context.Background())
	return err
}

func (db *Database) ReplaceMCPConnectionTools(connectionID uuid.UUID, tools []models.MCPConnectionTool) error {
	ctx := context.Background()
	return db.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().
			Model((*models.MCPConnectionTool)(nil)).
			Where("connection_id = ?", connectionID).
			Exec(ctx); err != nil {
			return err
		}
		if len(tools) == 0 {
			return nil
		}
		_, err := tx.NewInsert().Model(&tools).Exec(ctx)
		return err
	})
}

func (db *Database) GetMCPConnectionTools(connectionID string) ([]models.MCPConnectionTool, error) {
	var tools []models.MCPConnectionTool
	err := db.DB.NewSelect().
		Model(&tools).
		Where("connection_id = ?", connectionID).
		OrderExpr("name ASC").
		Scan(context.Background())
	return tools, err
}

func (db *Database) GetMCPConnectionToolByID(id string) (*models.MCPConnectionTool, error) {
	var tool models.MCPConnectionTool
	err := db.DB.NewSelect().
		Model(&tool).
		Where("id = ?", id).
		Scan(context.Background())
	return &tool, err
}

func (db *Database) GetMCPConnectionToolByPrefixedName(projectID, agentID, prefixedName string) (*models.MCPConnectionTool, *models.MCPConnection, *models.AgentMCPGrant, error) {
	var tool models.MCPConnectionTool
	err := db.DB.NewSelect().
		Model(&tool).
		Join("JOIN mcp_connections AS connection ON connection.id = mcp_connection_tool.connection_id").
		Join("JOIN agent_mcp_grants AS grant ON grant.mcp_connection_id = connection.id").
		Where("connection.app_project_id = ?", projectID).
		Where("connection.deleted_at IS NULL").
		Where("connection.status = ?", models.MCPConnectionStatusConnected).
		Where("grant.app_project_id = connection.app_project_id").
		Where("grant.agent_id = ?", agentID).
		Where("grant.enabled = TRUE").
		Where("mcp_connection_tool.prefixed_name = ?", prefixedName).
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, nil, nil, err
	}
	connection, err := db.GetMCPConnectionByID(tool.ConnectionID.String())
	if err != nil {
		return nil, nil, nil, err
	}
	grant, err := db.GetAgentMCPGrant(projectID, agentID, tool.ConnectionID.String())
	if err != nil {
		return nil, nil, nil, err
	}
	return &tool, connection, grant, nil
}

func (db *Database) GetAgentMCPGrant(projectID, agentID, connectionID string) (*models.AgentMCPGrant, error) {
	var grant models.AgentMCPGrant
	err := db.DB.NewSelect().
		Model(&grant).
		Where("app_project_id = ?", projectID).
		Where("agent_id = ?", agentID).
		Where("mcp_connection_id = ?", connectionID).
		Scan(context.Background())
	return &grant, err
}

func (db *Database) UpsertAgentMCPGrant(grant *models.AgentMCPGrant) (*models.AgentMCPGrant, error) {
	now := time.Now()
	if grant.CreatedAt.IsZero() {
		grant.CreatedAt = now
	}
	grant.UpdatedAt = now
	_, err := db.DB.NewInsert().
		Model(grant).
		On("CONFLICT (app_project_id, agent_id, mcp_connection_id) DO UPDATE").
		Set("enabled = EXCLUDED.enabled").
		Set("default_policy = EXCLUDED.default_policy").
		Set("tool_policies = EXCLUDED.tool_policies").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(context.Background())
	return grant, err
}

func (db *Database) GetAgentMCPGrantsForProject(projectID string) ([]models.AgentMCPGrant, error) {
	var grants []models.AgentMCPGrant
	err := db.DB.NewSelect().
		Model(&grants).
		Where("app_project_id = ?", projectID).
		OrderExpr("agent_id ASC, created_at ASC").
		Scan(context.Background())
	return grants, err
}

func (db *Database) GetAgentMCPGrantsForOrg(orgID string) ([]models.AgentMCPGrant, error) {
	var grants []models.AgentMCPGrant
	err := db.DB.NewSelect().
		Model(&grants).
		Join("JOIN app_projects AS app_project ON app_project.id = agent_mcp_grant.app_project_id").
		Where("app_project.organization_id = ?", orgID).
		Where("app_project.deleted_at IS NULL").
		OrderExpr("agent_mcp_grant.agent_id ASC, agent_mcp_grant.created_at ASC").
		Scan(context.Background())
	return grants, err
}

func (db *Database) GetGrantedMCPTools(projectID, agentID string) ([]models.MCPConnectionTool, error) {
	var tools []models.MCPConnectionTool
	err := db.DB.NewSelect().
		Model(&tools).
		Join("JOIN mcp_connections AS connection ON connection.id = mcp_connection_tool.connection_id").
		Join("JOIN agent_mcp_grants AS grant ON grant.mcp_connection_id = connection.id").
		Where("connection.app_project_id = ?", projectID).
		Where("connection.deleted_at IS NULL").
		Where("connection.status = ?", models.MCPConnectionStatusConnected).
		Where("grant.app_project_id = connection.app_project_id").
		Where("grant.agent_id = ?", agentID).
		Where("grant.enabled = TRUE").
		OrderExpr("connection.slug ASC, mcp_connection_tool.name ASC").
		Scan(context.Background())
	return tools, err
}

func (db *Database) CreateMCPToolApproval(approval *models.MCPToolApproval) (*models.MCPToolApproval, error) {
	_, err := db.DB.NewInsert().Model(approval).Exec(context.Background())
	return approval, err
}

func (db *Database) GetMCPToolApproval(id string) (*models.MCPToolApproval, error) {
	var approval models.MCPToolApproval
	err := db.DB.NewSelect().
		Model(&approval).
		Where("id = ?", id).
		Scan(context.Background())
	return &approval, err
}

func (db *Database) UpdateMCPToolApprovalDecision(approvalID string, status string, decidedBy uuid.UUID) error {
	res, err := db.DB.NewUpdate().
		Model((*models.MCPToolApproval)(nil)).
		Set("status = ?", status).
		Set("decided_by_user_id = ?", decidedBy).
		Set("decision_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", approvalID).
		Where("status = ?", models.MCPApprovalStatusPending).
		Exec(context.Background())
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return fmt.Errorf("approval is no longer pending")
	}
	return nil
}

func (db *Database) ExpireMCPToolApproval(approvalID string) error {
	_, err := db.DB.NewUpdate().
		Model((*models.MCPToolApproval)(nil)).
		Set("status = ?", models.MCPApprovalStatusExpired).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", approvalID).
		Where("status = ?", models.MCPApprovalStatusPending).
		Exec(context.Background())
	return err
}

func (db *Database) CreateAgentMCPToolCall(call *models.AgentMCPToolCall) (*models.AgentMCPToolCall, error) {
	_, err := db.DB.NewInsert().Model(call).Exec(context.Background())
	return call, err
}

func (db *Database) GetAgentMCPToolCallByApprovalID(approvalID string) (*models.AgentMCPToolCall, error) {
	var call models.AgentMCPToolCall
	err := db.DB.NewSelect().
		Model(&call).
		Where("approval_id = ?", approvalID).
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(context.Background())
	return &call, err
}

func (db *Database) UpdateAgentMCPToolCall(call *models.AgentMCPToolCall) (*models.AgentMCPToolCall, error) {
	call.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(call).WherePK().Exec(context.Background())
	return call, err
}
