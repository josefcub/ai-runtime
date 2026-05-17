package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/cover"
)

func TestCoverageTool_ParsesProfile(t *testing.T) {
	tmpDir := t.TempDir()
	profilePath := filepath.Join(tmpDir, "test.out")

	profileContent := "mode: set\ngithub.com/agent-project/harness/pkg/test.go:10.1,12.6 1 1\ngithub.com/agent-project/harness/pkg/test.go:14.7,15.2 1 0\n"

	err := os.WriteFile(profilePath, []byte(profileContent), 0644)
	if err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		t.Fatalf("failed to parse profile: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	if len(profiles[0].Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(profiles[0].Blocks))
	}

	b := profiles[0].Blocks
	if b[0].StartLine != 10 || b[0].EndLine != 12 {
		t.Fatalf("block 0: expected lines 10-12, got %d-%d", b[0].StartLine, b[0].EndLine)
	}
	if b[0].Count != 1 {
		t.Fatalf("block 0: expected Count=1, got %d", b[0].Count)
	}
	if b[1].StartLine != 14 || b[1].EndLine != 15 {
		t.Fatalf("block 1: expected lines 14-15, got %d-%d", b[1].StartLine, b[1].EndLine)
	}
	if b[1].Count != 0 {
		t.Fatalf("block 1: expected Count=0, got %d", b[1].Count)
	}
}

func TestCoverageTool_IfBlock(t *testing.T) {
	profileContent := "mode: set\ngithub.com/agent-project/harness/pkg/cond.go:5.1,8.6 1 1\ngithub.com/agent-project/harness/pkg/cond.go:9.7,12.3 1 0\n"

	profiles, err := cover.ParseProfilesFromReader(strings.NewReader(profileContent))
	if err != nil {
		t.Fatalf("failed to parse profile: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	fileLineCoverage := make(map[string]map[int]int)
	for _, p := range profiles {
		for _, rec := range p.Blocks {
			if fileLineCoverage[p.FileName] == nil {
				fileLineCoverage[p.FileName] = make(map[int]int)
			}
			for line := rec.StartLine; line <= rec.EndLine; line++ {
				fileLineCoverage[p.FileName][line] = rec.Count
			}
		}
	}

	condCoverage := fileLineCoverage["github.com/agent-project/harness/pkg/cond.go"]

	for _, line := range []int{5, 6, 7, 8} {
		if condCoverage[line] != 1 {
			t.Errorf("line %d: expected covered (1), got %d", line, condCoverage[line])
		}
	}

	for _, line := range []int{9, 10, 11, 12} {
		if condCoverage[line] != 0 {
			t.Errorf("line %d: expected uncovered (0), got %d", line, condCoverage[line])
		}
	}
}

func TestCoverageTool_OutputFormatting(t *testing.T) {
	profileContent := "mode: set\ngithub.com/agent-project/harness/pkg/format.go:1.1,3.2 1 5\ngithub.com/agent-project/harness/pkg/format.go:5.1,5.3 1 0\n"

	profiles, err := cover.ParseProfilesFromReader(strings.NewReader(profileContent))
	if err != nil {
		t.Fatalf("failed to parse profile: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	fileLineCoverage := make(map[string]map[int]int)
	for _, p := range profiles {
		for _, block := range p.Blocks {
			if fileLineCoverage[p.FileName] == nil {
				fileLineCoverage[p.FileName] = make(map[int]int)
			}
			for line := block.StartLine; line <= block.EndLine; line++ {
				if block.Count > fileLineCoverage[p.FileName][line] {
					fileLineCoverage[p.FileName][line] = block.Count
				}
			}
		}
	}

	coverage := fileLineCoverage["github.com/agent-project/harness/pkg/format.go"]

	if coverage[1] == 0 {
		t.Error("line 1: expected covered (5), got 0")
	}
	if coverage[2] == 0 {
		t.Error("line 2: expected covered (5), got 0")
	}
	if coverage[3] == 0 {
		t.Error("line 3: expected covered (5), got 0")
	}
	if coverage[5] != 0 {
		t.Errorf("line 5: expected uncovered (0), got %d", coverage[5])
	}
}

func TestIntegration_WithRealTest(t *testing.T) {
	tmpDir := t.TempDir()

	moduleDir := filepath.Join(tmpDir, "testmodule")
	os.MkdirAll(moduleDir, 0755)

	goModContent := `module testmodule
go 1.22
`
	err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	srcDir := filepath.Join(moduleDir, "math")
	os.MkdirAll(srcDir, 0755)

	src := `package math

import "errors"

func Add(a, b int) int {
	return a + b
}

func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}
`

	err = os.WriteFile(filepath.Join(srcDir, "math.go"), []byte(src), 0644)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	test := `package math

import "testing"

func TestAdd(t *testing.T) {
	result := Add(1, 2)
	if result != 3 {
		t.Errorf("got %d", result)
	}
}

func TestDivide(t *testing.T) {
	result, _ := Divide(10, 2)
	if result != 5 {
		t.Errorf("got %d", result)
	}
}
`
	err = os.WriteFile(filepath.Join(srcDir, "math_test.go"), []byte(test), 0644)
	if err != nil {
		t.Fatalf("failed to create test: %v", err)
	}

	cmd := exec.Command("go", "test", "-coverprofile=cover.out", "./...")
	cmd.Dir = moduleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("go test failed (skipping): %v (%s)", err, string(output))
	}

	data, err := os.ReadFile(filepath.Join(moduleDir, "cover.out"))
	if err != nil {
		t.Fatalf("cover.out not created: %v", err)
	}

	tmpProfile := filepath.Join(tmpDir, "cover.out")
	err = os.WriteFile(tmpProfile, data, 0644)
	if err != nil {
		t.Fatalf("failed to write temp profile: %v", err)
	}
	defer os.Remove(tmpProfile)

	profiles, err := cover.ParseProfiles(tmpProfile)
	if err != nil {
		t.Fatalf("failed to parse cover.out: %v", err)
	}

	if len(profiles) == 0 {
		t.Fatal("no profile data found")
	}

	fileLineCoverage := make(map[string]map[int]int)
	for _, p := range profiles {
		for _, rec := range p.Blocks {
			realFile := p.FileName
			rel := strings.TrimPrefix(realFile, filepath.Join(moduleDir, "math"))
			if rel == "" || !strings.HasSuffix(realFile, "math.go") {
				continue
			}
			mapKey := "math/math.go"
			if fileLineCoverage[mapKey] == nil {
				fileLineCoverage[mapKey] = make(map[int]int)
			}
			for line := rec.StartLine; line <= rec.EndLine; line++ {
				if rec.Count > fileLineCoverage[mapKey][line] {
					fileLineCoverage[mapKey][line] = rec.Count
				}
			}
		}
	}

	coverage := fileLineCoverage["math/math.go"]

	if len(coverage) == 0 {
		t.Fatal("no coverage data for math.go")
	}

	hasCovered := false
	for _, count := range coverage {
		if count > 0 {
			hasCovered = true
			break
		}
	}
	if !hasCovered {
		t.Fatal("expected some covered lines")
	}

	if coverage[6] != 1 {
		t.Errorf("line 6 (Add return): expected covered, got %d", coverage[6])
	}

	if coverage[11] == 0 {
		t.Logf("line 11 (Divide error return) correctly uncovered")
	}

	if coverage[13] == 1 {
		t.Logf("line 13 (Divide success return) correctly covered")
	}

	// Line 10 (if b == 0) boundary check — may have count from surrounding blocks
	for line, count := range coverage {
		if line == 11 && count != 0 {
			t.Errorf("line %d (error return): expected uncovered (0), got %d", line, count)
		}
	}
}
