package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const defaultExts = ".asm .c .cc .cpp .csv .go .h .hh .hpp .json .rs .s .sql .v .vhdl .zig"

var processes []*exec.Cmd

var lastRun time.Time

var opts struct {
	exts         string
	patterns     string
	skipDotDirs  bool
	skipDotFiles bool
	skipPatterns string
	interval     time.Duration
	verbose      bool
	clear        bool
	clearCmd     string
	sigterm      bool
	cmds         []string
}

func main() {
	flag.StringVar(&opts.exts, "exts", defaultExts, "A space separated list of file extensions to watch")
	flag.StringVar(&opts.patterns, "patterns", "", "A space separated list of patterns to watch")
	flag.BoolVar(&opts.skipDotDirs, "skip-dot-dirs", true, "Whether to automatically skip any directories that begin with a dot")
	flag.BoolVar(&opts.skipDotFiles, "skip-dot-files", false, "Whether to automatically skip any files that begin with a dot")
	flag.StringVar(&opts.skipPatterns, "skip-patterns", "node_modules/*", "A space separated list of patterns to skip")
	flag.DurationVar(&opts.interval, "interval", 2*time.Second, "The interval to check for file changes")
	flag.BoolVar(&opts.verbose, "verbose", false, "Print the commands that are about to be executed")
	flag.BoolVar(&opts.clear, "clear", false, "Clear the terminal before running commands")
	flag.StringVar(&opts.clearCmd, "clear-cmd", "", "An optional command to run to clear the terminal")
	flag.BoolVar(&opts.sigterm, "sigterm", false, "On linux/mac use SIGTERM instead of SIGKILL")
	flag.Parse()

	const defaultsPrefix = "+ "
	if strings.HasPrefix(opts.exts, defaultsPrefix) {
		opts.exts = strings.Replace(opts.exts, defaultsPrefix, defaultExts+" ", 1)
	}

	opts.patterns = strings.TrimSpace(opts.patterns)
	opts.skipPatterns = strings.TrimSpace(opts.skipPatterns)

	var cmds []string
	for _, str := range flag.Args() {
		if strings.HasPrefix(str, "make:") {
			str = strings.TrimPrefix(str, "make:")

			for _, str := range strings.Split(str, ",") {
				str = strings.TrimSpace("make " + strings.TrimSpace(str))

				cmds = append(cmds, str)
			}
		} else {
			cmds = append(cmds, str)
		}
	}

	exts := make(map[string]struct{})
	for _, ext := range strings.Fields(opts.exts) {
		if ext == "" {
			continue
		}

		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}

		exts[ext] = struct{}{}
	}

	skipPatterns := strings.Fields(opts.skipPatterns)
	watchPatterns := strings.Fields(opts.patterns)
	skip := func(path string, entry fs.DirEntry) bool {
		if path == "." {
			return true
		}

		if strings.HasPrefix(entry.Name(), ".") {
			skipDir := entry.IsDir() && opts.skipDotDirs
			skipFile := !entry.IsDir() && opts.skipDotFiles

			if skipDir || skipFile {
				return true
			}
		}

		path = filepath.ToSlash(path)

		for _, pattern := range skipPatterns {
			matched, err := filepath.Match(pattern, path)
			if err != nil {
				fmt.Printf("watch skip pattern error: %v\n", err)
			}
			if matched {
				return true
			}
		}

		if _, ok := exts[filepath.Ext(path)]; !entry.IsDir() && !ok {
			return true
		}

		for _, pattern := range watchPatterns {
			matched, err := filepath.Match(pattern, path)
			if err != nil {
				fmt.Printf("watch pattern error: %v\n", err)
			}
			if matched {
				return false
			}
		}

		return false
	}

	var numFiles int
	var lastNumFiles int
	files := make(map[string]time.Time)
	for {
		var shouldRun bool

		_ = filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if skip(path, entry) {
				// Completely skip directories
				if entry.IsDir() && path != "." {
					return filepath.SkipDir
				}

				// Skip files individually
				return nil
			}

			fi, err := entry.Info()
			if err != nil {
				return err
			}

			numFiles++

			if modified, ok := files[path]; !shouldRun && ok {
				shouldRun = modified.Before(fi.ModTime()) && lastRun.Before(fi.ModTime())
			}

			files[path] = fi.ModTime()

			return nil
		})

		shouldRun = shouldRun || numFiles != lastNumFiles

		if shouldRun {
			run(cmds)
		}

		lastNumFiles = numFiles
		numFiles = 0

		time.Sleep(opts.interval)
	}
}

func run(cmdStrs []string) {
	lastRun = time.Now()

	if opts.clear {
		clear()
	}

	// Kill any running processes
	for _, cmd := range processes {
		switch runtime.GOOS {
		case "windows":
			pid := strconv.Itoa(cmd.Process.Pid)

			exec.Command("taskkill", "/t", "/f", "/pid", pid).Run()

		default:
			if opts.sigterm {
				cmd.Process.Signal(syscall.SIGTERM)
			} else {
				cmd.Process.Kill()
			}
		}
	}

	processes = nil

	// Rather than writing a parser for nested command line args we use this
	// regular expression
	// It should be fine for most use cases where it matches:
	// - Escaped double quotes:  "(\\"|[^"])+"
	// - Space separated values: [^\s\\]+
	// - Escaped spaces:         (\\+\s[^\s\\]+)*
	re := regexp.MustCompile(`"(\\"|[^"])+"|[^\s\\]+(\\+\s[^\s\\]+)*`)

	// Run command strings
	for i, cmdStr := range cmdStrs {
		fields := re.FindAllString(cmdStr, -1)
		for i := range fields {
			fields[i] = strings.ReplaceAll(fields[i], `\ `, " ")
			fields[i] = strings.ReplaceAll(fields[i], `\"`, `"`)
			fields[i] = strings.ReplaceAll(fields[i], `\\`, `\`)
		}

		program, args, message := command(fields[0], fields[1:]...)

		if opts.verbose {
			fmt.Println(message)
		}

		cmd := exec.Command(program, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		processes = append(processes, cmd)

		if i == len(cmdStrs)-1 {
			if err := cmd.Start(); err != nil {
				fmt.Println(err)

				break
			}
		} else {
			if err := cmd.Run(); err != nil {
				fmt.Println(err)

				break
			}
		}
	}
}

func clear() {
	if opts.clearCmd != "" {
		cmd := exec.Command(opts.clearCmd)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		cmd.Run()
	} else {
		fmt.Print("\033c")
	}
}

func command(program string, args ...string) (string, []string, string) {
	messageValues := make([]any, len(args))
	for i, arg := range args {
		messageValues[i] = arg
	}

	verbs := make([]string, len(args))
	for i, arg := range args {
		if strings.IndexFunc(arg, unicode.IsSpace) >= 0 {
			verbs[i] = "%q"
		} else {
			verbs[i] = "%v"
		}
	}
	message := fmt.Sprintf("%v "+strings.Join(verbs, " "), append([]any{program}, messageValues...)...)

	return program, args, message
}
