package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/cover"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	args := os.Args[1:]
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printUsage(os.Stdout)
			os.Exit(0)
		}
	}

	if len(args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	profilePath := args[0]
	srcDir := args[1]
	var pkgPattern string
	if len(args) >= 3 {
		pkgPattern = strings.TrimPrefix(args[2], "./")
	}

	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read profile: %v\n", err)
		os.Exit(1)
	}

	if len(profiles) == 0 {
		fmt.Println("no coverage data")
		return
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

	files := make([]string, 0, len(fileLineCoverage))
	for f := range fileLineCoverage {
		files = append(files, f)
	}
	sort.Strings(files)

	modulePrefix := "github.com/agent-project/harness/"
	relFiles := make([]string, 0, len(files))
	fullFromRel := make(map[string]string)
	for _, f := range files {
		if !skipVendor(f) {
			rel := strings.TrimPrefix(f, modulePrefix)
			relFiles = append(relFiles, rel)
			fullFromRel[rel] = f
		}
	}

	if pkgPattern != "" {
		var filtered []string
		for _, rf := range relFiles {
			if strings.Contains(rf, pkgPattern) {
				filtered = append(filtered, rf)
			}
		}
		relFiles = filtered
	}

	for i, relFile := range relFiles {
		if i > 0 {
			fmt.Println("-----")
		}
		absPath := srcDir + "/" + relFile
		src, err := os.ReadFile(absPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", relFile, err)
			continue
		}

		lines := strings.Split(string(src), "\n")
		fullPath := fullFromRel[relFile]
		coverage := fileLineCoverage[fullPath]
		totalLines := len(lines)
		coveredLines := 0
		for i, line := range lines {
			if len(line) == 0 {
				continue
			}
			if coverage[i+1] > 0 {
				coveredLines++
			}
		}
		var pct int
		if totalLines > 0 {
			pct = (coveredLines*100 + totalLines - 1) / totalLines
		}

		fmt.Printf("--- %s (%d%% covered, %d/%d lines)\n", relFile, pct, coveredLines, totalLines)
		for lineNum, line := range lines {
			nums := lineNum + 1
			count := coverage[nums]
			if count == 0 && len(line) > 0 {
				fmt.Printf("  %d:  %s  // uncovered\n", nums, line)
			} else {
				fmt.Printf("  %d:  %d  %s\n", nums, count, line)
			}
		}
	}
}

func skipVendor(path string) bool {
	if strings.HasPrefix(path, "vendor/") || strings.Contains(path, "/vendor/") {
		return true
	}
	if strings.HasPrefix(path, "golang.org/x/") || strings.HasPrefix(path, "google.golang.org/") {
		return true
	}
	if !strings.HasPrefix(path, "github.com/agent-project/") {
		return true
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, "usage: coverage [flags] <profile.out> <src_dir> [<package_pattern>]\n\n")
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  -h, --help    show this help\n\n")
	fmt.Fprintf(w, "Arguments:\n")
	fmt.Fprintf(w, "  profile.out       coverage profile file\n")
	fmt.Fprintf(w, "  src_dir           source directory\n")
	fmt.Fprintf(w, "  package_pattern   optional substring to filter files by prefix\n")
	fmt.Fprintf(w, "                    files matching golang.org/x/, google.golang.org/, github.com/\n")
	fmt.Fprintf(w, "                    or vendor/ paths are excluded\n")
}
