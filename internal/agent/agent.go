// Package agent implements an autonomous coding agent that interprets LLM
// replies, extracts file-write and shell-command code blocks, executes them
// against the local filesystem (subject to sandbox rules), and feeds results
// back into the conversation loop until the model signals AUTO_DONE or the
// iteration limit is reached.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

// MaxIterations is the default maximum number of autonomous loop iterations
// before the agent returns an error.
const MaxIterations = 20

// Options control the behaviour of the autonomous agent loop.
type Options struct {
	Goal       string // free-form objective the agent should accomplish
	Sandbox    bool   // enable sandbox restrictions on shell commands
	WorkDir    string // working directory for file writes and commands
	LiveOutput bool   // stream LLM tokens to stdout during iteration
}

// Session lets the agent append messages and snapshot the conversation
// history without depending on a concrete session package.
type Session interface {
	Append(msg llm.ChatMessage)
	Snapshot() []llm.ChatMessage
}

// SendFunc is the signature for the LLM call the agent invokes on each
// iteration. The first return value is the engine name, the second is the
// reply text, and the error indicates a non-retryable failure.
type SendFunc func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error)

// Notify is called by the agent to emit progress messages (e.g. "wrote file",
// "running command") during an iteration.
type Notify func(line string)

// ParseGoal extracts an optional iteration-count override from the goal text
// (e.g. "5 iterations fix the bugs" → ("fix the bugs", 5)). When no override
// is present, the default MaxIterations is returned.
func ParseGoal(input string) (string, int) {
	// empty goal means use defaults
	if input == "" {
		return input, MaxIterations
	}
	re := mustCompile(`(?i)(?:max\s+)?(\d+)\s+iterations?`)
	matches := re.FindStringSubmatch(input)
	if len(matches) > 1 {
		if cMax, err := strconv.Atoi(matches[1]); err == nil {
			cleanedGoal := re.ReplaceAllString(input, "")
			return trimSpace(cleanedGoal), cMax
		}
	}
	return input, MaxIterations
}

// Run executes the autonomous agent loop. On each iteration it sends the
// conversation history to the LLM via send, parses the reply for fenced file
// and run blocks, executes them, appends execution feedback to the history,
// and loops until the model signals AUTO_DONE or MaxIterations is reached.
func Run(ctx context.Context, opts Options, hist Session, send SendFunc, notify Notify) error {
	if hist == nil {
		return errors.New("agent: history must not be nil")
	}
	if send == nil {
		return errors.New("agent: send function must not be nil")
	}
	if notify == nil {
		return errors.New("agent: notify function must not be nil")
	}
	_, totalIterations := ParseGoal(opts.Goal)
	everActed := false
	for iteration := 1; iteration <= totalIterations; iteration++ {
		notify("--- auto iteration " + strconv.Itoa(iteration) + "/" + strconv.Itoa(totalIterations) + " ---")

		var tokenPrinter llm.StreamFunc
		if opts.LiveOutput {
			tokenPrinter = func(tok string) {
				fmt.Print(tok)
			}
		}

		_, reply, err := send(ctx, hist.Snapshot(), tokenPrinter)
		if err != nil {
			return err
		}
		hist.Append(llm.ChatMessage{Role: "assistant", Content: reply})

		if issues := detectMalformedAttempts(reply); len(issues) > 0 {
			notify("warning: possible malformed block")
		}

		var feedback []string
		fileBlocks := parseFileBlocks(reply)
		for _, block := range fileBlocks {
			targetFile := trimSpace(block.filename)
			fileContent := block.content
			resolved, err := resolveWritePath(opts.WorkDir, targetFile)
			if err != nil {
				notify("  edit REFUSED: " + err.Error())
				feedback = append(feedback, "REFUSED: "+err.Error())
				continue
			}
			if err := writeFile(resolved, fileContent); err != nil {
				feedback = append(feedback, "FAILED: "+err.Error())
			} else {
				notify("  wrote " + targetFile)
				feedback = append(feedback, "SUCCESS: "+targetFile)
			}
		}

		runBlocks := parseRunBlocks(reply)
		for _, block := range runBlocks {
			cmdStr := trimSpace(block)
			notify("  running: " + cmdStr)
			out, err := runCommand(ctx, cmdStr, opts.WorkDir, opts.Sandbox)
			if containsStr(out, "COMMAND REFUSED") {
				notify("  run REFUSED (sandbox policy)")
				feedback = append(feedback, out)
			} else {
				notify("  -> ok")
				if err != nil {
					feedback = append(feedback, "ERROR:\n"+out+"\n"+err.Error())
				} else {
					feedback = append(feedback, "OUTPUT:\n"+out)
				}
			}
		}

		hasActed := len(fileBlocks) > 0 || len(runBlocks) > 0
		if hasActed {
			everActed = true
		}

		if doneRe.MatchString(reply) {
			if !everActed {
				notify("  re-prompting once: AUTO_DONE received with no file/run activity yet")
				hist.Append(llm.ChatMessage{
					Role: "user",
					Content: "You signaled AUTO_DONE but no file or command changes have " +
						"been made yet in this session. Either make the concrete change " +
						"the goal requires, or explicitly confirm no changes are needed, " +
						"then signal AUTO_DONE again.",
				})
				everActed = true
				continue
			}
			notify("agent signaled AUTO_DONE")
			return nil
		}

		if len(feedback) == 0 {
			feedback = append(feedback, "No actions executed.")
		}
		hist.Append(llm.ChatMessage{Role: "user", Content: joinStr(feedback, "\n")})
	}
	return errors.New("max iterations reached")
}
