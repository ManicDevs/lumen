package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

const MaxIterations = 20

type Options struct {
	Goal       string
	Sandbox    bool
	WorkDir    string
	LiveOutput bool
}

type Session interface {
	Append(msg llm.ChatMessage)
	Snapshot() []llm.ChatMessage
}

type SendFunc func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error)

type Notify func(line string)

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

func Run(ctx context.Context, opts Options, hist Session, send SendFunc, notify Notify) error {
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
