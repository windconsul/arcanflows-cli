package api

// Platform skills (PROPOSAL-031) and direct skill runs (PROPOSAL-121 S2).

import (
	"encoding/json"
	"fmt"
	"strings"
)

type SkillInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
}

// SkillRunInputs is what starting a procedure skill on an agent takes.
type SkillRunInputs struct {
	Workflow string       `json:"workflow"`
	Inputs   []SkillInput `json:"inputs"`
	Hint     string       `json:"hint"`
}

type Skill struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Slug            string       `json:"slug"`
	Description     string       `json:"description"`
	Category        string       `json:"category"`
	TriggerPhrase   string       `json:"trigger_phrase"`
	Instruction     string       `json:"instruction"`
	EntryWorkflowID *string      `json:"entry_workflow_id,omitempty"`
	Inputs          []SkillInput `json:"inputs"`
	Version         int          `json:"version"`
	IsActive        bool         `json:"is_active"`
	IsSystem        bool         `json:"is_system"`
	IsProcedure     bool         `json:"is_procedure"`
	Degraded        []string     `json:"degraded"`
	AttachedAgents  int          `json:"attached_agents"`
	EntryWorkflow   string       `json:"entry_workflow_name,omitempty"`
}

type AgentSkill struct {
	SkillID         string `json:"skill_id"`
	SkillVersion    int    `json:"skill_version"`
	IsEnabled       bool   `json:"is_enabled"`
	UpdateAvailable bool   `json:"update_available"`
	Skill           *Skill `json:"skill,omitempty"`
}

type PlanStep struct {
	Name string `json:"name"`
}

type SkillRun struct {
	ExecutionID string         `json:"execution_id"`
	Workflow    string         `json:"workflow"`
	Slug        string         `json:"slug"`
	Plan        []PlanStep     `json:"plan"`
	EstimateUSD *float64       `json:"estimate_usd,omitempty"`
	Queued      bool           `json:"queued,omitempty"`
	QueueID     *string        `json:"queue_id,omitempty"`
	Position    int            `json:"position,omitempty"`
	Inputs      map[string]any `json:"inputs"`
}

type SkillRunStatus struct {
	ExecutionID string   `json:"execution_id"`
	Status      string   `json:"status"`
	CurrentNode string   `json:"current_node,omitempty"`
	StepsDone   int      `json:"steps_done"`
	ElapsedSec  int      `json:"elapsed_seconds"`
	Error       string   `json:"error,omitempty"`
	Links       []string `json:"links,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	CostUSD     float64  `json:"cost_usd"`
}

type SkillQueueItem struct {
	Status      string  `json:"status"`
	Position    int     `json:"position,omitempty"`
	QueuedAhead int     `json:"queued_ahead"`
	ExecutionID *string `json:"execution_id,omitempty"`
	Error       string  `json:"error,omitempty"`
}

// Done reports whether a run has finished, one way or another.
func (s SkillRunStatus) Done() bool {
	switch s.Status {
	case "completed", "failed", "cancelled", "timed_out", "skipped":
		return true
	}
	return false
}

func (c *Client) ListSkills() ([]Skill, error) {
	var out struct {
		Skills []Skill `json:"skills"`
	}
	err := c.Get("/api/v1/skills", &out)
	return out.Skills, err
}

func (c *Client) ListAgentSkills(agentID string) ([]AgentSkill, error) {
	var out struct {
		Skills []AgentSkill `json:"skills"`
	}
	err := c.Get("/api/v1/agents/"+agentID+"/skills", &out)
	return out.Skills, err
}

func (c *Client) StartSkillRun(agentID, skillID string, inputs map[string]any, source string) (*SkillRun, error) {
	var out SkillRun
	body := map[string]any{"inputs": inputs}
	if strings.TrimSpace(source) != "" {
		body["source"] = source
	}
	err := c.Post("/api/v1/agents/"+agentID+"/skills/"+skillID+"/runs", body, &out)
	return &out, err
}

func (c *Client) GetSkillRun(agentID, skillID, execID string) (*SkillRunStatus, error) {
	var out SkillRunStatus
	err := c.Get("/api/v1/agents/"+agentID+"/skills/"+skillID+"/runs/"+execID, &out)
	return &out, err
}

func (c *Client) StopSkillRun(agentID, skillID, execID string) error {
	return c.Delete("/api/v1/agents/" + agentID + "/skills/" + skillID + "/runs/" + execID)
}

func (c *Client) GetSkillRunInputs(agentID, skillID string) (*SkillRunInputs, error) {
	var out SkillRunInputs
	err := c.Get("/api/v1/agents/"+agentID+"/skills/"+skillID+"/run-inputs", &out)
	return &out, err
}

func (c *Client) GetSkillQueueItem(queueID string) (*SkillQueueItem, error) {
	var out SkillQueueItem
	err := c.Get("/api/v1/workflow-executions/queue/"+queueID, &out)
	return &out, err
}

// ErrorMessage turns "API error 422: {\"error\":\"…\"}" into the server's own
// sentence; anything else comes back unchanged.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.Index(s, "{"); i >= 0 && strings.HasPrefix(s, "API error") {
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(s[i:]), &body) == nil {
			if body.Error != "" {
				return body.Error
			}
			if body.Message != "" {
				return body.Message
			}
		}
	}
	return s
}

// SkillMatches reports whether ref names this skill (id, slug, slug without
// its numeric creation suffix, or name — case-insensitive).
func SkillMatches(sk *Skill, ref string) bool {
	if sk == nil {
		return false
	}
	r := strings.ToLower(strings.TrimSpace(ref))
	slug := strings.ToLower(sk.Slug)
	if r == strings.ToLower(sk.ID) || r == slug || r == strings.ToLower(sk.Name) {
		return true
	}
	// Slugs get a numeric suffix on create ("research-brief-1790865620"):
	// "research-brief" matches that, "research" does not.
	if rest, ok := strings.CutPrefix(slug, r+"-"); ok && rest != "" && strings.Trim(rest, "0123456789") == "" {
		return true
	}
	return false
}

func (s *Skill) Kind() string {
	if s.IsProcedure || s.EntryWorkflowID != nil {
		return "procedure"
	}
	return "contract"
}

func (r SkillRun) String() string {
	return fmt.Sprintf("%s (%s)", r.Workflow, r.ExecutionID)
}
