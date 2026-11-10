package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unsetEnv removes key for the duration of the test, restoring any prior
// value afterwards (t.Setenv cannot express "absent").
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestVisionEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"local with URL", Config{VisionMode: "local", VisionLocalURL: "http://localhost:8080"}, true},
		{"local without URL", Config{VisionMode: "local"}, false},
		{"local with URL and cloud fields", Config{VisionMode: "local", VisionLocalURL: "http://x", VisionAPIURL: "https://api", VisionAPIKey: "k"}, true},
		{"cloud with URL and key", Config{VisionMode: "cloud", VisionAPIURL: "https://api", VisionAPIKey: "k"}, true},
		{"cloud without key", Config{VisionMode: "cloud", VisionAPIURL: "https://api"}, false},
		{"cloud without URL", Config{VisionMode: "cloud", VisionAPIKey: "k"}, false},
		{"empty mode with cloud fields", Config{VisionAPIURL: "https://api", VisionAPIKey: "k"}, true},
		{"empty config", Config{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.VisionEnabled(); got != tc.want {
				t.Fatalf("VisionEnabled=%v want %v", got, tc.want)
			}
		})
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("COV_ORDEF_SET", "real")
	unsetEnv(t, "COV_ORDEF_UNSET")
	if got := envOrDefault("COV_ORDEF_SET", "fb"); got != "real" {
		t.Fatalf("set got %q", got)
	}
	if got := envOrDefault("COV_ORDEF_UNSET", "fb"); got != "fb" {
		t.Fatalf("unset got %q", got)
	}
	// Set-but-empty also falls back (Getenv semantics).
	t.Setenv("COV_ORDEF_EMPTY", "")
	if got := envOrDefault("COV_ORDEF_EMPTY", "fb"); got != "fb" {
		t.Fatalf("empty got %q", got)
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		val  string
		fb   bool
		want bool
	}{
		{"true", false, true},
		{"1", false, true},
		{" TRUE ", false, true},
		{"TrUe", false, true},
		{"false", true, false},
		{"0", true, false},
		{" FALSE ", true, false},
		{"yes", true, true},
		{"yes", false, false},
		{"", true, true},
		{"", false, false},
		{"2", true, true},
	}
	for _, tc := range tests {
		t.Setenv("COV_BOOL", tc.val)
		if got := envBool("COV_BOOL", tc.fb); got != tc.want {
			t.Errorf("envBool(%q, fb=%v)=%v want %v", tc.val, tc.fb, got, tc.want)
		}
	}
	unsetEnv(t, "COV_BOOL_ABSENT")
	if got := envBool("COV_BOOL_ABSENT", true); !got {
		t.Errorf("absent with fb=true got %v", got)
	}
}

func TestEnvInt(t *testing.T) {
	tests := []struct {
		val  string
		want int
	}{
		{"42", 42},
		{" 7 ", 7},
		{"1", 1},
		{"abc", 10},
		{"", 10},
		{"0", 10},
		{"-3", 10},
		{"3.5", 10},
		{"999999", 999999},
	}
	for _, tc := range tests {
		t.Setenv("COV_INT", tc.val)
		if got := envInt("COV_INT", 10); got != tc.want {
			t.Errorf("envInt(%q)=%d want %d", tc.val, got, tc.want)
		}
	}
	unsetEnv(t, "COV_INT_ABSENT")
	if got := envInt("COV_INT_ABSENT", 10); got != 10 {
		t.Errorf("absent got %d want 10", got)
	}
}

func TestPortInt(t *testing.T) {
	tests := []struct {
		port string
		want int
	}{
		{"9090", 9090},
		{"8080", 8080},
		{"1", 1},
		{"", 8080},
		{"abc", 8080},
		{"0", 8080},
		{"-1", -1},
	}
	for _, tc := range tests {
		if got := (&Config{Port: tc.port}).PortInt(); got != tc.want {
			t.Errorf("PortInt(%q)=%d want %d", tc.port, got, tc.want)
		}
	}
}

func TestLoadDotEnvFileBranches(t *testing.T) {
	// Missing file: silent no-op.
	loadDotEnvFile(filepath.Join(t.TempDir(), "nope.env"))

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"# a comment",
		"",
		"   ",
		"NO_EQUALS_SIGN",
		"=empty-key-value",
		"COV_DOTENV_A=hello",
		"COV_DOTENV_B=\"quoted\"",
		"COV_DOTENV_C='single'",
		"  COV_DOTENV_D  =  spaced value  ",
		"COV_DOTENV_E=with=equals",
		"COV_DOTENV_PRESENT=from-file",
		"COV_DOTENV_F=crlf-value\r",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"COV_DOTENV_A", "COV_DOTENV_B", "COV_DOTENV_C", "COV_DOTENV_D", "COV_DOTENV_E", "COV_DOTENV_F"} {
		unsetEnv(t, k)
	}
	t.Setenv("COV_DOTENV_PRESENT", "from-env")

	loadDotEnvFile(path)

	want := map[string]string{
		"COV_DOTENV_A":       "hello",
		"COV_DOTENV_B":       "\"quoted", // Trim cuts trailing quote of whole line
		"COV_DOTENV_C":       "'single",
		"COV_DOTENV_D":       "spaced value",
		"COV_DOTENV_E":       "with=equals",
		"COV_DOTENV_F":       "crlf-value",
		"COV_DOTENV_PRESENT": "from-env", // pre-existing env wins
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("os.Getenv(%s)=%q want %q", k, got, v)
		}
	}
	// Empty key was skipped: no env var with empty name can exist; just
	// ensure the file was otherwise processed (covered above).
}

func TestResolveVisionGGUFBranches(t *testing.T) {
	dir := t.TempDir()

	// Glob with no matches returns raw.
	nomatch := filepath.Join(dir, "nope", "*.gguf")
	if got := resolveVisionGGUF(nomatch, "mmproj"); got != nomatch {
		t.Fatalf("glob no-match got %q want raw", got)
	}
	// Glob pattern with ? and [ characters.
	snap := filepath.Join(dir, "snap")
	makeGGUF(t, filepath.Join(snap, "model-v1.gguf"))
	q := filepath.Join(snap, "model-v?.gguf")
	if got := resolveVisionGGUF(q, "mmproj"); got != filepath.Join(snap, "model-v1.gguf") {
		t.Fatalf("? glob got %q", got)
	}
	br := filepath.Join(snap, "model-v[0-9].gguf")
	if got := resolveVisionGGUF(br, "mmproj"); got != filepath.Join(snap, "model-v1.gguf") {
		t.Fatalf("[ glob got %q", got)
	}

	// Glob matching only a directory named *.gguf: skipped, returns raw.
	d := filepath.Join(dir, "dironly")
	if err := os.MkdirAll(filepath.Join(d, "fake.gguf"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(d, "*.gguf")
	if got := resolveVisionGGUF(g, "mmproj"); got != g {
		t.Fatalf("glob dir-only got %q want raw %q", got, g)
	}

	// Directory containing no .gguf files returns the raw dir path.
	empty := filepath.Join(dir, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(empty, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveVisionGGUF(empty, "mmproj"); got != empty {
		t.Fatalf("dir without gguf got %q want raw", got)
	}
	// Directory containing only mmproj files: excluded -> raw.
	only := filepath.Join(dir, "onlyproj")
	makeGGUF(t, filepath.Join(only, "mmproj-x.gguf"))
	if got := resolveVisionGGUF(only, "mmproj"); got != only {
		t.Fatalf("dir only-mmproj got %q want raw", got)
	}
	// Whitespace-trimmed input resolves normally.
	trimmed := "  " + filepath.Join(snap, "model-v1.gguf") + "  "
	if got := resolveVisionGGUF(trimmed, "mmproj"); got != filepath.Join(snap, "model-v1.gguf") {
		t.Fatalf("trimmed got %q", got)
	}
	// Uppercase MMProj in glob is excluded for main model (case-insensitive).
	up := filepath.Join(dir, "up")
	makeGGUF(t, filepath.Join(up, "MMProj-BF16.gguf"))
	makeGGUF(t, filepath.Join(up, "main.gguf"))
	if got := resolveVisionGGUF(filepath.Join(up, "*.gguf"), "mmproj"); got != filepath.Join(up, "main.gguf") {
		t.Fatalf("uppercase mmproj glob got %q", got)
	}
}

func TestVisionPickInDirBranches(t *testing.T) {
	// Nonexistent dir -> "".
	if got := visionPickInDir(filepath.Join(t.TempDir(), "nope"), "mmproj"); got != "" {
		t.Fatalf("nonexistent dir got %q", got)
	}
	// A file (not a dir) -> ReadDir error -> "".
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := visionPickInDir(f, "mmproj"); got != "" {
		t.Fatalf("file path got %q", got)
	}

	dir := t.TempDir()
	// Subdirectory, non-gguf files and a bigger non-gguf file are ignored.
	if err := os.MkdirAll(filepath.Join(dir, "sub.gguf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSized := func(name string, size int) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSized("big.txt", 5000)
	writeSized("small.gguf", 10)
	writeSized("large.gguf", 1000)
	writeSized("UPPER.GGUF", 2000)
	writeSized("mmproj-huge.gguf", 99999)

	// Largest non-mmproj wins, extension check is case-insensitive.
	if got := visionPickInDir(dir, "mmproj"); got != filepath.Join(dir, "UPPER.GGUF") {
		t.Fatalf("pick largest got %q want UPPER.GGUF", got)
	}
	// No exclusion prefix: the huge mmproj file now wins.
	if got := visionPickInDir(dir, ""); got != filepath.Join(dir, "mmproj-huge.gguf") {
		t.Fatalf("pick no-exclude got %q", got)
	}
	// A broken symlink named *.gguf: ReadDir lists it but Info() fails
	// (target missing), exercising the Info-error continue branch.
	dir2 := t.TempDir()
	if err := os.Symlink(filepath.Join(dir2, "missing-target.gguf"), filepath.Join(dir2, "broken.gguf")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	makeGGUF(t, filepath.Join(dir2, "real.gguf"))
	if got := visionPickInDir(dir2, "mmproj"); got != filepath.Join(dir2, "real.gguf") {
		t.Fatalf("broken symlink pick got %q want real.gguf", got)
	}
}

func TestVisionMmprojInDirBranches(t *testing.T) {
	// Nonexistent dir -> "".
	if got := visionMmprojInDir(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Fatalf("nonexistent dir got %q", got)
	}
	// A file (not a dir) -> "".
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := visionMmprojInDir(f); got != "" {
		t.Fatalf("file path got %q", got)
	}

	dir := t.TempDir()
	writeSized := func(name string, size int) {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Directory named like a projection file is skipped; non-gguf ignored.
	if err := os.MkdirAll(filepath.Join(dir, "mmproj-dir.gguf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSized("mmproj-notes.txt", 5)
	writeSized("model.gguf", 100)
	// Case-insensitive name match; ReadDir order -> first match wins.
	writeSized("z-MMProj-upper.gguf", 10)
	writeSized("a-mmproj-first.gguf", 10)
	if got := visionMmprojInDir(dir); got != filepath.Join(dir, "a-mmproj-first.gguf") {
		t.Fatalf("mmproj pick got %q want a-mmproj-first", got)
	}
}

func TestResolveVisionMmprojBranches(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	model := filepath.Join(snap, "model.gguf")
	makeGGUF(t, model)
	makeGGUF(t, filepath.Join(snap, "mmproj-BF16.gguf"))

	// modelPath is a directory: search that directory itself.
	if got := resolveVisionMmproj("", snap); got != filepath.Join(snap, "mmproj-BF16.gguf") {
		t.Fatalf("dir modelPath got %q", got)
	}
	// modelPath does not exist: Dir(modelPath) has no mmproj -> "".
	if got := resolveVisionMmproj("", filepath.Join(dir, "ghost", "model.gguf")); got != "" {
		t.Fatalf("ghost modelPath got %q want empty", got)
	}
	// Explicit mmproj given as glob.
	if got := resolveVisionMmproj(filepath.Join(snap, "mmproj-*.gguf"), model); got != filepath.Join(snap, "mmproj-BF16.gguf") {
		t.Fatalf("glob mmproj got %q", got)
	}
	// Explicit mmproj glob with no matches: resolveVisionGGUF returns the
	// raw pattern (non-empty), so resolveVisionMmproj passes it through
	// rather than falling back to auto-find.
	rawGlob := filepath.Join(dir, "nope", "*.gguf")
	if got := resolveVisionMmproj(rawGlob, model); got != rawGlob {
		t.Fatalf("unresolvable explicit mmproj got %q want raw glob", got)
	}
}
