package autoagent

import (
	"context"
	"errors"
	"fmt"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/engine"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const MaxIterations = 20

var (
	doneRe = regexp.MustCompile(
		"(?i)AUTO_DONE",
	)
	placeholderPathRe = regexp.MustCompile(
		"[<>]|RELATIVE_PATH_TO_THE_FILE",
	)
	// denylistPatterns are matched against the raw command string in
	// runCommand when sandbox mode is on. Word-boundary (\b) regexes are
	// used instead of plain substring checks (the previous approach) for
	// two reasons: substring matching both under- and over-blocks --
	// "sudo\t" or "sudo("-style spacing tricks slipped past a literal
	// "sudo " match, while a harmless command name like
	// "recreate_chmod_docs.sh" would have been refused for merely
	// containing "chmod " as a substring. Regexes with \b close both
	// gaps. This is still a denylist, not a sandbox in the security
	// sense -- it catches known-dangerous shapes, not arbitrary damage,
	// so it should not be relied on as the only safeguard against a
	// misbehaving or adversarial model.
	denylistPatterns = []*regexp.Regexp{
		// Privilege escalation / permission and ownership changes.
		regexp.MustCompile(`(?i)\bsudo\b`),
		regexp.MustCompile(`(?i)\bchmod\b`),
		regexp.MustCompile(`(?i)\bchown\b`),
		// System power state.
		regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`),
		// Filesystem formatting and raw disk writes.
		regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`),
		regexp.MustCompile(`(?i)\bdd\s+.*\bif=`),
		regexp.MustCompile(`(?i)>\s*/dev/(sd|nvme|hd|disk|xvd)`),
		// Fork bomb.
		regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
		// Piping a remote script straight into a shell interpreter.
		regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba)?sh\b`),
		// Killing init / mass process termination.
		regexp.MustCompile(`(?i)\bkill\s+(-9\s+)?1\b`),
		regexp.MustCompile(`(?i)\bkillall\s+-9\b`),
		// Overwriting core system/auth files.
		regexp.MustCompile(`(?i)>\s*/etc/(passwd|shadow|sudoers)\b`),
	}
	sandboxEnvKeys = []string{
		"PATH", "LANG", "LC_ALL", "HOME",
	}
	defaultSandboxPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	systemAddendum     = "Guidance: Always try " +
		"to run go files with `go run .` directly."
)

type Options struct {
	Goal    string
	Sandbox bool
	WorkDir string
}

type Session interface {
	Append(msg engine.ChatMessage)
	Snapshot() []engine.ChatMessage
}

type SendFunc func(
	ctx context.Context,
	history []engine.ChatMessage,
	onToken engine.StreamFunc,
) (string, string, error)

type Notify func(line string)

type fileBlock struct {
	filename string
	content  string
}

func ParseAutoPrompt(
	input string,
) (string, int) {
	re := regexp.MustCompile(
		"(?i)(?:max\\s+)?(\\d+)\\s+iterations?",
	)
	matches := re.FindStringSubmatch(input)
	if len(matches) > 1 {
		if cMax, err := strconv.Atoi(
			matches[1],
		); err == nil {
			cleanedGoal := re.ReplaceAllString(
				input, "",
			)
			return strings.TrimSpace(
				cleanedGoal,
			), cMax
		}
	}
	return input, MaxIterations
}

func SanitizeFilename(
	filename string,
) string {
	base := strings.TrimSpace(filename)
	if base == ".go" || base == "go" {
		return "main.go"
	}
	if strings.HasPrefix(base, ".") &&
		!strings.Contains(base, "/") &&
		len(base) <= 5 {
		return "main" + base
	}
	return base
}

func matchDenylist(cmd string) bool {
	for _, pattern := range denylistPatterns {
		if pattern.MatchString(cmd) {
			return true
		}
	}
	return matchDangerousRM(cmd)
}

// matchDangerousRM looks for an "rm" invocation that combines a recursive
// flag, a force flag, and a dangerous target (root, home, or an
// unqualified wildcard) -- the "rm -rf /" family. This is deliberately a
// token-level check rather than a single regex: rm's recursive/force
// flags can be spelled as a combined short form ("-rf", "-fr"), separate
// short forms ("-r -f"), or long forms ("--recursive", "--force") in
// either order, and a single regex that accepts all of those
// combinations while still rejecting benign commands gets unreadable
// fast. Scanning tokens after each "rm" is both correct and easy to
// verify by inspection.
func matchDangerousRM(cmd string) bool {
	tokens := strings.Fields(cmd)
	for i, tok := range tokens {
		if tok != "rm" {
			continue
		}
		recursive, force, dangerous := false, false, false
		for _, t := range tokens[i+1:] {
			// A shell control operator ends this particular rm
			// invocation; anything after it belongs to the next command.
			if t == ";" || t == "|" || t == "&" || t == "&&" || t == "||" {
				break
			}
			switch {
			case t == "--recursive":
				recursive = true
			case t == "--force":
				force = true
			case strings.HasPrefix(t, "--"):
				// Some other long flag (e.g. --interactive); not
				// dangerous on its own, ignore it.
			case strings.HasPrefix(t, "-") && len(t) > 1:
				if strings.ContainsAny(t, "rR") {
					recursive = true
				}
				if strings.Contains(t, "f") {
					force = true
				}
			case t == "/" || t == "~" || t == "$HOME" || t == "*":
				dangerous = true
			case strings.HasPrefix(t, "/"):
				dangerous = true
			}
		}
		if recursive && force && dangerous {
			return true
		}
	}
	return false
}

func getFence() string {
	return string([]byte{96, 96, 96})
}

func parseFileBlocks(
	reply string,
) []fileBlock {
	var blocks []fileBlock
	lines := strings.Split(reply, "\n")
	inBlock := false
	var currentFile string
	var currentContent []string
	f := getFence()
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(
				trimmed, f+"file:",
			) {
				inBlock = true
				currentFile = strings.TrimPrefix(
					trimmed, f+"file:",
				)
				currentContent = []string{}
			} else if strings.HasPrefix(
				trimmed, f+"go",
			) {
				inBlock = true
				currentFile = "main.go"
				currentContent = []string{}
			}
		} else {
			if trimmed == f {
				contentStr := strings.Join(
					currentContent, "\n",
				) + "\n"
				blocks = append(
					blocks, fileBlock{
						filename: currentFile,
						content:  contentStr,
					},
				)
				inBlock = false
			} else {
				currentContent = append(
					currentContent, line,
				)
			}
		}
	}
	return blocks
}

func parseRunBlocks(reply string) []string {
	var blocks []string
	lines := strings.Split(reply, "\n")
	inBlock := false
	var currentContent []string
	f := getFence()
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, f+"run") ||
				strings.HasPrefix(trimmed, f+"sh") ||
				strings.HasPrefix(trimmed, f+"bash") {
				inBlock = true
				currentContent = []string{}
			}
		} else {
			if trimmed == f {
				contentStr := strings.Join(
					currentContent, "\n",
				) + "\n"
				blocks = append(
					blocks, contentStr,
				)
				inBlock = false
			} else {
				currentContent = append(
					currentContent, line,
				)
			}
		}
	}
	return blocks
}

func detectMalformedAttempts(
	reply string,
) []string {
	var issues []string
	f := getFence()
	fences := strings.Count(reply, f)
	if fences%2 != 0 {
		issues = append(
			issues, "odd number of code fences",
		)
	}
	fileOpens := 0
	lines := strings.Split(reply, "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, f+"file:") ||
			strings.HasPrefix(t, f+"go") {
			fileOpens++
		}
	}
	if fileOpens != len(parseFileBlocks(reply)) {
		issues = append(
			issues, "opened file block never closed",
		)
	}
	runOpens := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, f+"run") ||
			strings.HasPrefix(t, f+"sh") ||
			strings.HasPrefix(t, f+"bash") {
			runOpens++
		}
	}
	if runOpens != len(parseRunBlocks(reply)) {
		issues = append(
			issues, "opened run block never closed",
		)
	}
	return issues
}

func resolveWritePath(
	workDir, target string,
) (string, error) {
	target = SanitizeFilename(target)
	if placeholderPathRe.MatchString(target) {
		return "", errors.New(
			"edit REFUSED: template placeholder",
		)
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	finalPath := filepath.Join(absWork, target)
	absFinal, err := filepath.Abs(finalPath)
	if err != nil {
		return "", err
	}
	// A plain strings.HasPrefix(absFinal, absWork) check here would wrongly
	// accept a sibling directory that merely shares absWork's string
	// prefix -- e.g. absWork "/tmp/sandbox" and a "../sandbox-evil/x"
	// target resolving to "/tmp/sandbox-evil/x", which does have
	// "/tmp/sandbox" as a string prefix without being inside it at all.
	// Requiring an exact match or a prefix followed by the path separator
	// closes that gap while still accepting absFinal == absWork itself.
	if absFinal != absWork && !strings.HasPrefix(absFinal, absWork+string(filepath.Separator)) {
		return "", errors.New(
			"write REFUSED: outside sandbox",
		)
	}
	return absFinal, nil
}

func writeFile(
	path, content string,
) error {
	if err := os.MkdirAll(
		filepath.Dir(path), 0755,
	); err != nil {
		return err
	}
	return os.WriteFile(
		path, []byte(content), 0644,
	)
}

func shPath() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

func runCommand(
	ctx context.Context,
	cmdStr string,
	workDir string,
	sandbox bool,
) (string, error) {
	if sandbox && matchDenylist(cmdStr) {
		return "COMMAND REFUSED: sandbox restriction", nil
	}
	cmd := exec.CommandContext(
		ctx, shPath(), "-c", cmdStr,
	)
	cmd.Dir = workDir
	if sandbox {
		var env []string
		pathSet := false
		for _, key := range sandboxEnvKeys {
			if val, exists := os.LookupEnv(key); exists {
				env = append(env, key+"="+val)
				if key == "PATH" {
					pathSet = true
				}
			}
		}
		if !pathSet {
			env = append(env, "PATH="+defaultSandboxPath)
		}
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

func Run(
	ctx context.Context,
	opts Options,
	hist Session,
	send SendFunc,
	notify Notify,
) error {
	_, totalIterations := ParseAutoPrompt(
		opts.Goal,
	)
	everActed := false
	for iteration := 1; iteration <= totalIterations; iteration++ {
		notify("--- auto iteration " +
			strconv.Itoa(iteration) + "/" +
			strconv.Itoa(totalIterations) + " ---")
		// Stream tokens to stdout as they arrive, just like the interactive
		// chat path does. This prevents `/auto` from appearing stuck while
		// waiting for the full response to complete.
		tokenPrinter := func(tok string) {
			fmt.Print(tok)
		}
		_, reply, err := send(
			ctx, hist.Snapshot(), tokenPrinter,
		)
		if err != nil {
			return err
		}
		hist.Append(engine.ChatMessage{
			Role:    "assistant",
			Content: reply,
		})
		if issues := detectMalformedAttempts(
			reply,
		); len(issues) > 0 {
			notify("warning: possible malformed block")
		}
		var feedback []string
		fileBlocks := parseFileBlocks(reply)
		for _, block := range fileBlocks {
			targetFile := strings.TrimSpace(
				block.filename,
			)
			fileContent := block.content
			resolved, err := resolveWritePath(
				opts.WorkDir, targetFile,
			)
			if err != nil {
				notify("  edit REFUSED: " +
					err.Error())
				feedback = append(
					feedback, "REFUSED: "+
						err.Error(),
				)
				continue
			}
			if err := writeFile(
				resolved, fileContent,
			); err != nil {
				feedback = append(
					feedback, "FAILED: "+
						err.Error(),
				)
			} else {
				notify("  wrote " + targetFile)
				feedback = append(
					feedback, "SUCCESS: "+
						targetFile,
				)
			}
		}
		runBlocks := parseRunBlocks(reply)
		for _, block := range runBlocks {
			cmdStr := strings.TrimSpace(block)
			notify("  running: " + cmdStr)
			out, err := runCommand(
				ctx, cmdStr, opts.WorkDir,
				opts.Sandbox,
			)
			if strings.Contains(
				out, "COMMAND REFUSED",
			) {
				notify("  run REFUSED (sandbox policy)")
				feedback = append(feedback, out)
			} else {
				notify("  -> ok")
				if err != nil {
					feedback = append(
						feedback, "ERROR:\n"+
							out+"\n"+err.Error(),
					)
				} else {
					feedback = append(
						feedback, "OUTPUT:\n"+out,
					)
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
				hist.Append(engine.ChatMessage{
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
			feedback = append(
				feedback, "No actions executed.",
			)
		}
		hist.Append(engine.ChatMessage{
			Role: "user",
			Content: strings.Join(
				feedback, "\n",
			),
		})
	}
	return errors.New("max iterations reached")
}

// ValidateCommand inspects dynamic script blocks for obfuscation arrays before system fork allocation
func ValidateCommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if strings.Contains(trimmed, "&&") || strings.Contains(trimmed, "||") || strings.Contains(trimmed, ";") {
		if strings.Contains(trimmed, "sudo") || strings.Contains(trimmed, "rm ") {
			return fmt.Errorf("dangerous command chaining permutation blocked by agent policy")
		}
	}
	return nil
}
