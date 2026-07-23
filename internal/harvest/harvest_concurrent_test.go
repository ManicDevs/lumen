package harvest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMinifyCode_ConcurrentSafety(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, 20)
	for i := range files {
		ext := ".go"
		if i%3 == 0 {
			ext = ".py"
		} else if i%3 == 1 {
			ext = ".sql"
		}
		src := "// comment " + string(rune('0'+i%10)) + "\nvar x = " + string(rune('0'+i%10)) + "\n"
		if ext == ".py" {
			src = "# comment " + string(rune('0'+i%10)) + "\nx = " + string(rune('0'+i%10)) + "\n"
		} else if ext == ".sql" {
			src = "-- comment " + string(rune('0'+i%10)) + "\nSELECT " + string(rune('0'+i%10)) + ";\n"
		}
		path := filepath.Join(dir, "file_"+string(rune('0'+i))+ext)
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		files[i] = path
	}

	var wg sync.WaitGroup
	for _, path := range files {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			result, err := MinifyCode(p)
			if err != nil {
				t.Errorf("MinifyCode(%s): %v", p, err)
				return
			}
			if result == "" {
				t.Errorf("MinifyCode(%s): empty result", p)
			}
		}(path)
	}
	wg.Wait()
}

func TestContext_ConcurrentDirectories(t *testing.T) {
	dirs := make([]string, 10)
	for i := range dirs {
		dir := t.TempDir()
		for j := 0; j < 5; j++ {
			src := "// file " + string(rune('A'+j)) + "\nvar x" + string(rune('0'+j)) + " = " + string(rune('0'+j)) + "\n"
			name := "file_" + string(rune('A'+j)) + ".go"
			if j%2 == 0 {
				name = "file_" + string(rune('A'+j)) + ".py"
				src = "# file " + string(rune('A'+j)) + "\nx" + string(rune('0'+j)) + " = " + string(rune('0'+j)) + "\n"
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
				t.Fatal(err)
			}
		}
		dirs[i] = dir
	}

	var wg sync.WaitGroup
	for _, dir := range dirs {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			result, err := Context(d)
			if err != nil {
				t.Errorf("Context(%s): %v", d, err)
				return
			}
			if result == "" {
				t.Errorf("Context(%s): empty result", d)
			}
		}(dir)
	}
	wg.Wait()
}
