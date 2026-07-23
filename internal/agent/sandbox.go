package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	placeholderPathRe = regexp.MustCompile(`[<>]|RELATIVE_PATH_TO_THE_FILE`)

	denylistPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsudo\b`),
		regexp.MustCompile(`(?i)\bchmod\b`),
		regexp.MustCompile(`(?i)\bchown\b`),
		regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`),
		regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`),
		regexp.MustCompile(`(?i)\bdd\s+.*\bif=`),
		regexp.MustCompile(`(?i)>\s*/dev/(sd|nvme|hd|disk|xvd)`),
		regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
		regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba)?sh\b`),
		regexp.MustCompile(`(?i)\bkill\s+(-9\s+)?1\b`),
		regexp.MustCompile(`(?i)\bkillall\s+-9\b`),
		regexp.MustCompile(`(?i)>\s*/etc/(passwd|shadow|sudoers)\b`),
	}

	sandboxEnvKeys     = []string{"PATH", "LANG", "LC_ALL", "HOME"}
	defaultSandboxPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

func matchDangerousRM(cmd string) bool {
	tokens := strings.Fields(cmd)
	for i, tok := range tokens {
		if tok != "rm" {
			continue
		}
		recursive, force, dangerous := false, false, false
		for _, t := range tokens[i+1:] {
			if t == ";" || t == "|" || t == "&" || t == "&&" || t == "||" {
				break
			}
			switch {
			case t == "--recursive":
				recursive = true
			case t == "--force":
				force = true
			case strings.HasPrefix(t, "--"):
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

func matchDenylist(cmd string) bool {
	for _, pattern := range denylistPatterns {
		if pattern.MatchString(cmd) {
			return true
		}
	}
	return matchDangerousRM(cmd)
}

func resolveWritePath(workDir, target string) (string, error) {
	target = SanitizeFilename(target)
	if placeholderPathRe.MatchString(target) {
		return "", errors.New("edit REFUSED: template placeholder")
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
	if absFinal != absWork && !strings.HasPrefix(absFinal, absWork+string(filepath.Separator)) {
		return "", errors.New("write REFUSED: outside sandbox")
	}
	return absFinal, nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func shPath() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

func runCommand(ctx context.Context, cmdStr, workDir string, sandbox bool) (string, error) {
	if sandbox && matchDenylist(cmdStr) {
		return "COMMAND REFUSED: sandbox restriction", nil
	}
	cmd := exec.CommandContext(ctx, shPath(), "-c", cmdStr)
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
