// Project Manager bot — integrates Matrix, Ollama, Gitea, and OnlyOffice.
//
// Commands:
//
//	!help                     - Show all commands
//	!repos                    - List Gitea repositories
//	!issues <repo>            - List open issues for a repo
//	!projects                 - List OnlyOffice projects
//	!tasks <project>          - List tasks for an OnlyOffice project
//	!create-task <project> | <title> | <description>
//	                          - Create a new OnlyOffice task
//	!summarize <repo>         - AI summary of open issues
//	!ai <prompt>              - Ask the AI anything
//
// Environment variables:
//
//	export MATRIX_API_URL="https://matrix.example.com"
//	export MATRIX_API_USER="botuser"
//	export MATRIX_API_PASS="botpassword"
//	export GITEA_URL="https://gitea.example.com"
//	export GITEA_TOKEN="your-token"
//	export GITEA_OWNER="your-org"
//	export ONLYOFFICE_URL="https://office.example.com"
//	export ONLYOFFICE_USER="admin@example.com"
//	export ONLYOFFICE_PASS="password"
//	export OPEN_WEB_API_GENERATE_URL="http://localhost:11434/api/generate"
//	export OPEN_WEB_API_TOKEN="your-ollama-token"
//	go run ./examples/project-manager/
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	matrix "github.com/eslider/go-matrix-bot"
	gitea "github.com/eslider/go-gitea-helpers"
	ollama "github.com/eslider/go-ollama"
	onlyoffice "github.com/eslider/go-onlyoffice"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// services holds all connected service clients.
type services struct {
	bot *matrix.Bot
	ai  *ollama.Client        // optional
	git *gitea.Client          // optional
	oo  *onlyoffice.Client     // optional

	giteaOwner string
}

func main() {
	// --- Matrix bot (required) ---
	botConfig := matrix.GetEnvironmentConfig()
	botConfig.Debug = true

	if botConfig.Homeserver == "" {
		fmt.Fprintln(os.Stderr, "MATRIX_API_URL is not set")
		os.Exit(1)
	}

	bot, err := matrix.NewBot(botConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create bot: %v\n", err)
		os.Exit(1)
	}

	svc := &services{bot: bot}

	// --- Ollama (optional) ---
	if url := os.Getenv("OPEN_WEB_API_GENERATE_URL"); url != "" {
		svc.ai = ollama.NewOpenWebUiClient(&ollama.DSN{
			URL:   url,
			Token: os.Getenv("OPEN_WEB_API_TOKEN"),
		})
		fmt.Println("[+] Ollama AI connected")
	}

	// --- Gitea (optional) ---
	giteaCfg := gitea.GetEnvironmentConfig()
	if giteaCfg.URL != "" {
		svc.git, err = gitea.NewClient(giteaCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Gitea error: %v\n", err)
		} else {
			svc.giteaOwner = giteaCfg.Owner
			fmt.Println("[+] Gitea connected:", giteaCfg.URL)
		}
	}

	// --- OnlyOffice (optional) ---
	ooCreds := onlyoffice.GetEnvironmentCredentials()
	if ooCreds.Url != "" {
		svc.oo = onlyoffice.NewClient(ooCreds)
		fmt.Println("[+] OnlyOffice connected:", ooCreds.Url)
	}

	// --- Register command handler ---
	bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
		body := strings.TrimSpace(msg.Body)
		if !strings.HasPrefix(body, "!") {
			return
		}

		parts := strings.SplitN(body, " ", 2)
		cmd := strings.ToLower(parts[0])
		args := ""
		if len(parts) > 1 {
			args = strings.TrimSpace(parts[1])
		}

		switch cmd {
		case "!help":
			svc.cmdHelp(ctx, roomID, sender)
		case "!repos":
			svc.cmdRepos(ctx, roomID, sender)
		case "!issues":
			svc.cmdIssues(ctx, roomID, sender, args)
		case "!projects":
			svc.cmdProjects(ctx, roomID, sender)
		case "!tasks":
			svc.cmdTasks(ctx, roomID, sender, args)
		case "!create-task":
			svc.cmdCreateTask(ctx, roomID, sender, args)
		case "!summarize":
			svc.cmdSummarize(ctx, roomID, sender, args)
		case "!ai":
			svc.cmdAI(ctx, roomID, sender, args)
		default:
			_ = bot.SendText(ctx, roomID, "Unknown command. Type !help")
		}
	})

	// --- Start ---
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("\nProject Manager bot starting... Type !help in a room.")
	fmt.Println("Press Ctrl+C to stop.")

	go func() {
		if runErr := bot.Run(ctx); runErr != nil {
			fmt.Fprintf(os.Stderr, "Bot error: %v\n", runErr)
			cancel()
		}
	}()

	<-ctx.Done()
	fmt.Println("\nShutting down...")
	_ = bot.Stop()
}

// --- Command handlers ---

func (s *services) cmdHelp(ctx context.Context, roomID id.RoomID, sender id.UserID) {
	md := `**Project Manager Bot — Commands**

| Command | Description |
|---|---|
| ` + "`!help`" + ` | Show this help |
| ` + "`!repos`" + ` | List Gitea repositories |
| ` + "`!issues <repo>`" + ` | List open issues for a repo |
| ` + "`!projects`" + ` | List OnlyOffice projects |
| ` + "`!tasks <project>`" + ` | List tasks for an OnlyOffice project |
| ` + "`!create-task <project> \\| <title> \\| <description>`" + ` | Create an OnlyOffice task |
| ` + "`!summarize <repo>`" + ` | AI summary of open issues |
| ` + "`!ai <prompt>`" + ` | Ask the AI anything |

**Services:** ` + s.statusLine()
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) statusLine() string {
	var parts []string
	if s.git != nil {
		parts = append(parts, "Gitea")
	}
	if s.oo != nil {
		parts = append(parts, "OnlyOffice")
	}
	if s.ai != nil {
		parts = append(parts, "Ollama AI")
	}
	if len(parts) == 0 {
		return "_none connected_"
	}
	return strings.Join(parts, ", ")
}

func (s *services) cmdRepos(ctx context.Context, roomID id.RoomID, sender id.UserID) {
	if s.git == nil {
		_ = s.bot.SendText(ctx, roomID, "Gitea is not configured.")
		return
	}

	repos, err := s.git.GetAllRepos(s.giteaOwner)
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Repositories** (%d):\n\n", len(repos)))
	for _, r := range repos {
		desc := r.Description
		if len(desc) > 60 {
			desc = desc[:60] + "..."
		}
		sb.WriteString(fmt.Sprintf("- **%s** — %s\n", r.Name, desc))
	}

	md := sb.String()
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) cmdIssues(ctx context.Context, roomID id.RoomID, sender id.UserID, repo string) {
	if s.git == nil {
		_ = s.bot.SendText(ctx, roomID, "Gitea is not configured.")
		return
	}
	if repo == "" {
		_ = s.bot.SendText(ctx, roomID, "Usage: `!issues <repo-name>`")
		return
	}

	issues, err := s.git.GetAllIssues(s.giteaOwner, repo)
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Issues for %s** (%d):\n\n", repo, len(issues)))
	for i, iss := range issues {
		if i >= 25 {
			sb.WriteString(fmt.Sprintf("\n_...and %d more_\n", len(issues)-25))
			break
		}
		state := "open"
		if iss.State == "closed" {
			state = "closed"
		}
		sb.WriteString(fmt.Sprintf("- #%d [%s] **%s**\n", iss.Index, state, iss.Title))
	}

	md := sb.String()
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) cmdProjects(ctx context.Context, roomID id.RoomID, sender id.UserID) {
	if s.oo == nil {
		_ = s.bot.SendText(ctx, roomID, "OnlyOffice is not configured.")
		return
	}

	projects, err := s.oo.GetProjects()
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**OnlyOffice Projects** (%d):\n\n", len(projects)))
	for _, p := range projects {
		tasks := 0
		if p.TaskCountTotal != nil {
			tasks = *p.TaskCountTotal
		}
		sb.WriteString(fmt.Sprintf("- **%s** — %d tasks\n", *p.Title, tasks))
	}

	md := sb.String()
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) cmdTasks(ctx context.Context, roomID id.RoomID, sender id.UserID, projectName string) {
	if s.oo == nil {
		_ = s.bot.SendText(ctx, roomID, "OnlyOffice is not configured.")
		return
	}
	if projectName == "" {
		_ = s.bot.SendText(ctx, roomID, "Usage: `!tasks <project-name>`")
		return
	}

	projects, err := s.oo.GetProjects()
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	project := projects.Get(projectName)
	if project == nil {
		_ = s.bot.SendText(ctx, roomID, fmt.Sprintf("Project '%s' not found.", projectName))
		return
	}

	tasks, err := s.oo.GetTasks(onlyoffice.NewProjectGetTasksRequest(*project.ID))
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Tasks for %s** (%d):\n\n", projectName, len(tasks)))
	for i, t := range tasks {
		if i >= 25 {
			sb.WriteString(fmt.Sprintf("\n_...and %d more_\n", len(tasks)-25))
			break
		}
		status := "open"
		if t.Status != nil && *t.Status == onlyoffice.ProjectTaskStatusClosed {
			status = "closed"
		}
		sb.WriteString(fmt.Sprintf("- [%s] **%s**\n", status, *t.Title))
	}

	md := sb.String()
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) cmdCreateTask(ctx context.Context, roomID id.RoomID, sender id.UserID, args string) {
	if s.oo == nil {
		_ = s.bot.SendText(ctx, roomID, "OnlyOffice is not configured.")
		return
	}

	// Parse: project | title | description
	parts := strings.SplitN(args, "|", 3)
	if len(parts) < 2 {
		_ = s.bot.SendText(ctx, roomID, "Usage: `!create-task <project> | <title> | <description>`")
		return
	}

	projectName := strings.TrimSpace(parts[0])
	title := strings.TrimSpace(parts[1])
	description := ""
	if len(parts) > 2 {
		description = strings.TrimSpace(parts[2])
	}

	projects, err := s.oo.GetProjects()
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	project := projects.Get(projectName)
	if project == nil {
		_ = s.bot.SendText(ctx, roomID, fmt.Sprintf("Project '%s' not found.", projectName))
		return
	}

	task, err := s.oo.CreateProjectTask(onlyoffice.NewProjectTaskRequest{
		ProjectId:   *project.ID,
		Title:       title,
		Description: description,
	})
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error creating task: "+err.Error())
		return
	}

	md := fmt.Sprintf("Task created: **%s** (ID: %d) in project **%s**", *task.Title, *task.ID, projectName)
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) cmdSummarize(ctx context.Context, roomID id.RoomID, sender id.UserID, repo string) {
	if s.git == nil || s.ai == nil {
		_ = s.bot.SendText(ctx, roomID, "Requires both Gitea and Ollama to be configured.")
		return
	}
	if repo == "" {
		_ = s.bot.SendText(ctx, roomID, "Usage: `!summarize <repo-name>`")
		return
	}

	issues, err := s.git.GetAllIssues(s.giteaOwner, repo)
	if err != nil {
		_ = s.bot.SendText(ctx, roomID, "Error: "+err.Error())
		return
	}

	// Build issue list for AI
	var issueSummary strings.Builder
	openCount := 0
	for _, iss := range issues {
		if iss.State == "closed" {
			continue
		}
		openCount++
		issueSummary.WriteString(fmt.Sprintf("- #%d: %s\n", iss.Index, iss.Title))
	}

	if openCount == 0 {
		_ = s.bot.SendText(ctx, roomID, fmt.Sprintf("No open issues in %s.", repo))
		return
	}

	prompt := fmt.Sprintf(
		"Summarize these %d open issues for the repository '%s'. "+
			"Group them by theme, highlight priorities, and suggest next steps:\n\n%s",
		openCount, repo, issueSummary.String(),
	)

	var chunks []string
	queryErr := s.ai.Query(ollama.Request{
		Model:  "llama3.2:3b",
		Prompt: prompt,
		Options: &ollama.RequestOptions{
			Temperature: ollama.Float(0.3),
		},
		OnJson: func(res ollama.Response) error {
			if res.Response != nil {
				chunks = append(chunks, *res.Response)
			}
			return nil
		},
	})

	if queryErr != nil {
		_ = s.bot.SendText(ctx, roomID, "AI error: "+queryErr.Error())
		return
	}

	response := strings.Join(chunks, "")
	md := fmt.Sprintf("**AI Summary for %s** (%d open issues):\n\n%s", repo, openCount, response)
	_ = s.bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
}

func (s *services) cmdAI(ctx context.Context, roomID id.RoomID, sender id.UserID, prompt string) {
	if s.ai == nil {
		_ = s.bot.SendText(ctx, roomID, "Ollama AI is not configured.")
		return
	}
	if prompt == "" {
		_ = s.bot.SendText(ctx, roomID, "Usage: `!ai <your question>`")
		return
	}

	var chunks []string
	queryErr := s.ai.Query(ollama.Request{
		Model:  "llama3.2:3b",
		Prompt: prompt,
		Options: &ollama.RequestOptions{
			Temperature: ollama.Float(0.7),
		},
		OnJson: func(res ollama.Response) error {
			if res.Response != nil {
				chunks = append(chunks, *res.Response)
			}
			return nil
		},
	})

	if queryErr != nil {
		_ = s.bot.SendText(ctx, roomID, "AI error: "+queryErr.Error())
		return
	}

	response := strings.Join(chunks, "")
	_ = s.bot.SendReply(ctx, roomID, response, matrix.MarkdownToHTML(response), sender)
}
