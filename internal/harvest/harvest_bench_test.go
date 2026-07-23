package harvest

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkMinifyCode_Go(b *testing.B) {
	dir := b.TempDir()
	src := "package main\n\n// comment\nfunc main() {\n\t// another comment\n\tprintln(\"hello\")\n}\n"
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := MinifyCode(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMinifyCode_Python(b *testing.B) {
	dir := b.TempDir()
	src := "def main():\n    # a comment\n    print('hello')\n    # another\n    return 42\n"
	path := filepath.Join(dir, "main.py")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := MinifyCode(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMinifyCode_LargeFile(b *testing.B) {
	dir := b.TempDir()
	var src string
	for i := 0; i < 1000; i++ {
		src += "// line " + string(rune('0'+i%10)) + "\nvar x" + string(rune('0'+i%10)) + " = " + string(rune('0'+i%10)) + "\n"
	}
	path := filepath.Join(dir, "large.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := MinifyCode(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContext_SingleFile(b *testing.B) {
	dir := b.TempDir()
	src := "package main\nfunc main() {}\n"
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Context(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContext_Directory(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 50; i++ {
		name := "file_" + string(rune('A'+i%26)) + ".go"
		if i%3 == 0 {
			name = "file_" + string(rune('A'+i%26)) + ".py"
		}
		src := "// file " + name + "\nvar x = " + string(rune('0'+i%10)) + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Context(dir)
		if err != nil {
			b.Fatal(err)
		}
	}
}
