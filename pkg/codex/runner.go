package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	defaultModel    = "gpt-5.4-codex"
	defaultSandbox  = "danger-full-access"
	defaultApproval = "never"
)

type Runner struct {
	commandPath string
	model       string
	sandbox     string
	approval    string
}

func NewRunner() *Runner {
	return &Runner{
		commandPath: "codex",
		model:       defaultModel,
		sandbox:     defaultSandbox,
		approval:    defaultApproval,
	}
}

func (r *Runner) Start(ctx context.Context, cwd, prompt string) (string, string, error) {
	args := []string{
		"exec",
		"--json",
		"-m", r.model,
		"-s", r.sandbox,
		"-c", fmt.Sprintf("approval_policy=%q", r.approval),
	}
	if strings.TrimSpace(cwd) != "" {
		args = append(args, "-C", strings.TrimSpace(cwd))
	}
	args = append(args, strings.TrimSpace(prompt))

	output, err := r.runCommand(ctx, r.commandPath, args)
	if err != nil {
		return "", "", err
	}
	threadID, message := parseCodexJSONL(output)
	return threadID, message, nil
}

func (r *Runner) Reply(ctx context.Context, cwd, threadID, prompt string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", fmt.Errorf("thread id is required")
	}
	replyArgs := []string{
		"--json",
		"-m", r.model,
		"-s", r.sandbox,
		"-c", fmt.Sprintf("approval_policy=%q", r.approval),
		"-C", strings.TrimSpace(cwd),
		"--thread", threadID,
		strings.TrimSpace(prompt),
	}
	output, err := r.runCommand(ctx, "codex-reply", replyArgs)
	if err != nil {
		var execErr *exec.Error
		if !errors.As(err, &execErr) || !errors.Is(execErr, exec.ErrNotFound) {
			return "", err
		}
		fallbackArgs := []string{
			"exec",
			"resume",
			"--json",
			"-m", r.model,
			"-c", fmt.Sprintf("approval_policy=%q", r.approval),
		}
		if strings.TrimSpace(cwd) != "" {
			fallbackArgs = append(fallbackArgs, "-C", strings.TrimSpace(cwd))
		}
		fallbackArgs = append(fallbackArgs, threadID, strings.TrimSpace(prompt))
		output, err = r.runCommand(ctx, r.commandPath, fallbackArgs)
		if err != nil {
			return "", err
		}
	}
	_, message := parseCodexJSONL(output)
	return message, nil
}

func (r *Runner) runCommand(ctx context.Context, cmd string, args []string) ([]byte, error) {
	command := exec.CommandContext(ctx, cmd, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run %s %s: %w\n%s", cmd, strings.Join(args, " "), err, string(output))
	}
	return output, nil
}

func parseCodexJSONL(raw []byte) (string, string) {
	var threadID string
	var lastAgentMessage string

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		if eventType == "thread.started" {
			if value, ok := event["thread_id"].(string); ok && strings.TrimSpace(value) != "" {
				threadID = value
			}
			continue
		}
		if eventType != "item.completed" {
			continue
		}
		item, ok := event["item"].(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := item["type"].(string)
		if itemType != "agent_message" {
			continue
		}
		text, _ := item["text"].(string)
		if strings.TrimSpace(text) != "" {
			lastAgentMessage = text
		}
	}

	if strings.TrimSpace(lastAgentMessage) == "" {
		lastAgentMessage = "执行完成。"
	}
	return threadID, lastAgentMessage
}
