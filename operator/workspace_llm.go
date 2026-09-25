package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLMClassificationRequest represents the input given to the LLM for classifying a file.
type LLMClassificationRequest struct {
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

// LLMClassificationResponse represents the structured decision returned by the LLM.
type LLMClassificationResponse struct {
	Domain          string `json:"domain"`           // Canonical domain
	TargetPath      string `json:"target_path"`      // Suggested path in OpenLia workspace
	ProposedContent string `json:"proposed_content"` // Adapted markdown conforming to domain template
	Rationale       string `json:"rationale"`        // Short explanation of the classification
}

// LLMPlanner provides an interface to LLM-driven migration analysis.
type LLMPlanner interface {
	ClassifyAndAdapt(ctx context.Context, req LLMClassificationRequest) (LLMClassificationResponse, error)
}

// SystemPromptForMigration generates the schema and instructions for LLM planning.
func SystemPromptForMigration() string {
	return `You are OpenLia's Personal OS Migration Assistant.
Your task is to analyze user notes and files from an external directory/vault and adapt them into OpenLia's canonical Personal OS workspace structure.

OpenLia canonical domains:
1. "tasks": Concrete physical actions. Follows tasks/task-template.md (# Title, ## Action, ## Status: todo, ## Context, ## Checklist / Substeps).
2. "projects": Bounded initiatives with deliverables and milestones. Follows projects/project-template.md (# Title, ## Objective, ## Status: active, ## Context & Constraints, ## Milestones, ## Next Tasks).
3. "goals": High-level aspirations and outcomes. Follows goals/goal-template.md (# Title, ## Objective, ## Status: active, ## Timeframe, ## Key Results & Milestones).
4. "areas": Enduring life domains & standards. Follows areas/area-template.md (# Title, ## Scope & Purpose, ## Standards & Principles, ## Recurring Responsibilities).
5. "decisions": Decisions, trade-offs, options considered. Follows decisions/decision-template.md (# Title, ## Question, ## Status: proposed/decided, ## Context And Constraints, ## Options Considered).
6. "monitors": Standing checks and watch rules. Follows monitors/monitor-template.md (# Title, ## Target Condition, ## Status: active, ## Watch Details).
7. "people": Contact & relationship profiles. Follows people/person-template.md (# Title, ## Relationship & Context, ## Important Dates, ## Open Loops & Commitments).
8. "ideas": Hypotheses, seeds, exploration concepts. Follows ideas/idea-template.md (# Title, ## Concept, ## Status: seed, ## Potential Value & Opportunity, ## Next Exploration Step).
9. "travel": Trips and itineraries. Follows travel/trip-template.md (# Title, ## Overview, ## Itinerary Outline, ## Packing & Preparation Checklist).
10. "shopping": Purchase research and items. Follows shopping/item-template.md (# Title, ## Assessment, ## Budget & Price Targets, ## Candidate Options).
11. "finance": Budget and financial reviews. Follows finance/finance-template.md (# Title, ## Scope & Period, ## Spending Summary & Targets).
12. "calendar": Event notes and meeting agendas. Follows calendar/event-note-template.md (# Title, ## Details, ## Purpose & Agenda, ## Discussion & Raw Notes, ## Follow-up Action Items).
13. "knowledge/claims": Reusable claims with frontmatter (claim, source, provenance, kind, status).
14. "knowledge": General durable notes, research, educational lessons, and curriculum.
15. "archive": Inactive records, past logs, completed tasks.
16. "inbox": Unclassified captures or ambiguous notes.

Output strictly valid JSON with keys:
- "domain": string (one of the canonical domains above)
- "target_path": string (e.g. "tasks/buy-groceries.md")
- "proposed_content": string (the markdown text adapted cleanly to the template while fully preserving original facts and notes)
- "rationale": string (1-sentence rationale)`
}

// FallbackDeterministicPlanner implements LLMPlanner using deterministic heuristics.
type FallbackDeterministicPlanner struct{}

func (f *FallbackDeterministicPlanner) ClassifyAndAdapt(ctx context.Context, req LLMClassificationRequest) (LLMClassificationResponse, error) {
	targetPath, domain, adaptedContent, rationale := ClassifyAndAdaptFile(req.RelativePath, []byte(req.Content))
	return LLMClassificationResponse{
		Domain:          domain,
		TargetPath:      targetPath,
		ProposedContent: adaptedContent,
		Rationale:       rationale,
	}, nil
}

// ComposeLLMPlanner calls Hermes agent or provider endpoint when available.
type ComposeLLMPlanner struct {
	Compose Compose
	Config  Config
}

func (c *ComposeLLMPlanner) ClassifyAndAdapt(ctx context.Context, req LLMClassificationRequest) (LLMClassificationResponse, error) {
	// If hermes service is not running or context timed out, fallback to deterministic
	if c.Compose.Config.ProjectName == "" || !c.Compose.ServiceRunning(ctx, "hermes") {
		fallback := &FallbackDeterministicPlanner{}
		return fallback.ClassifyAndAdapt(ctx, req)
	}

	prompt := fmt.Sprintf("%s\n\nClassify this file:\nRelative Path: %s\nContent:\n%s",
		SystemPromptForMigration(), req.RelativePath, req.Content)

	// Invoke hermes in container
	res, err := c.Compose.Run(ctx, "exec", "-T", "hermes", "hermes", "chat", "--oneshot", "-q", prompt)
	if err != nil {
		fallback := &FallbackDeterministicPlanner{}
		return fallback.ClassifyAndAdapt(ctx, req)
	}

	stdout := strings.TrimSpace(string(res.Stdout))
	// Try parsing JSON block from model response
	jsonStr := extractJSONBlock(stdout)
	var resp LLMClassificationResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil || resp.Domain == "" || resp.TargetPath == "" {
		fallback := &FallbackDeterministicPlanner{}
		return fallback.ClassifyAndAdapt(ctx, req)
	}
	return resp, nil
}

func extractJSONBlock(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "\n")
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "\n")
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}
	return s
}

// Ensure interface compatibility
var _ LLMPlanner = (*FallbackDeterministicPlanner)(nil)
var _ LLMPlanner = (*ComposeLLMPlanner)(nil)
