package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/eduard-lt/llamawizard/internal/download"
	"github.com/eduard-lt/llamawizard/internal/launchd"
	"github.com/eduard-lt/llamawizard/internal/pi"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// Link kinds returned by classifyLink.
const (
	linkDirect    = "direct"
	linkHFResolve = "hf-resolve"
	linkHFRepo    = "hf-repo"
	linkUnknown   = "unknown"
)

var quantRe = regexp.MustCompile(`(?i)(IQ[0-9]+(?:_[A-Z0-9]+)*|Q[0-9]+(?:_[A-Z0-9]+)*|F16|BF16|FP16)`)

// classifyLink inspects a URL and decides what it points to.
//
// It returns a kind, and for HuggingFace URLs the repo (owner/repo) and, for
// direct resolve links, the file path within the repo. A repo *page* URL
// (e.g. https://huggingface.co/unsloth/Qwen3.8-27B-GGUF) is classified as
// linkHFRepo with an empty filename, so it is never mistaken for a file.
func classifyLink(raw string) (kind, repo, filename string) {
	url := strings.TrimSpace(raw)

	if i := strings.IndexAny(url, "?#"); i >= 0 {
		url = url[:i]
	}
	url = strings.TrimRight(url, "/")

	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	host, path := url, ""
	if i := strings.Index(url, "/"); i >= 0 {
		host = url[:i]
		path = strings.TrimPrefix(url[i:], "/")
	}

	var segs []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}

	host = strings.ToLower(host)
	isHF := host == "huggingface.co" || host == "www.huggingface.co" || host == "hf.co"

	if isHF && len(segs) >= 2 {
		repo = segs[0] + "/" + segs[1]

		for i, s := range segs {
			if s == "resolve" && i+2 < len(segs) {
				return linkHFResolve, repo, strings.Join(segs[i+2:], "/")
			}
		}

		return linkHFRepo, repo, ""
	}

	last := ""
	if len(segs) > 0 {
		last = segs[len(segs)-1]
	} else {
		last = path
	}
	if strings.HasSuffix(strings.ToLower(last), ".gguf") {
		return linkDirect, "", last
	}

	return linkUnknown, "", ""
}

// extractName parses a display name from the args following a --link URL.
// It accepts both "--name <x>" and a bare positional name.
func extractName(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--name" && i+1 < len(args) {
			return args[i+1]
		}
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// slugFromName sanitizes a user-provided name into a filesystem-safe slug.
func slugFromName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

// slugFromFilename derives a slug from an artifact filename, keeping the
// model family, size, and quant (e.g. "Qwen3.8-27B-Q4_K_M.gguf" →
// "qwen3.8-27b-q4-k-m").
func slugFromFilename(filename string) string {
	name := filename
	if strings.HasSuffix(strings.ToLower(name), ".gguf") {
		name = name[:len(name)-5]
	}
	return slugFromName(name)
}

// deriveSlug derives a slug for a new model. When a name is given it is
// used as the base, but the quant is always appended so different
// quantizations of the same model never collide. It does not check against
// already-installed models: registerModel rejects a slug that is already in
// state, before anything is saved.
func deriveSlug(name, filename string) string {
	if name != "" {
		base := slugFromName(name)
		if q := quantFromFilename(filename); q != "" {
			return base + "-" + slugFromName(q)
		}
		return base
	}
	return slugFromFilename(filename)
}

// quantFromFilename extracts the quantization token from a GGUF filename
// (e.g. "Qwen3.8-27B-Q4_K_M.gguf" → "Q4_K_M"). Empty when unknown.
func quantFromFilename(filename string) string {
	return strings.ToUpper(quantRe.FindString(filename))
}

// validateGGUF checks that a downloaded file actually starts with the GGUF
// magic bytes, rejecting HTML pages and other non-model content.
func validateGGUF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening downloaded file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("downloaded file is too small to be a GGUF model: %w", err)
	}
	if string(magic[:]) != "GGUF" {
		return fmt.Errorf("downloaded file is not a valid GGUF model (magic %q) — the link likely points to a web page, not a model file", string(magic[:]))
	}
	return nil
}

func humanSize(b int64) string {
	if b <= 0 {
		return "unknown size"
	}
	return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
}

// addModelFromURL adds a model from a --link URL, classifying it and either
// resolving a repo page into a pickable list of files or downloading directly.
func addModelFromURL(rawURL, name string) {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}
	if st.Port == 0 {
		fmt.Println("Run 'llamawizard' first to set up the wizard.")
		os.Exit(1)
	}

	kind, repo, filename := classifyLink(rawURL)
	switch kind {
	case linkHFResolve:
		addFromHFResolve(st, repo, filename, name)
	case linkHFRepo:
		addFromHFRepo(st, repo, name)
	case linkDirect:
		addFromDirect(st, rawURL, filename, name)
	default:
		fmt.Fprintf(os.Stderr, "Unrecognized link: %s\n\n", rawURL)
		fmt.Println("A --link URL should be one of:")
		fmt.Println("  1. A direct file link:")
		fmt.Println("       https://huggingface.co/<owner>/<repo>/resolve/main/<file>.gguf")
		fmt.Println("  2. A HuggingFace repo page:")
		fmt.Println("       https://huggingface.co/<owner>/<repo>")
		fmt.Println("  3. Any other direct .gguf URL")
		fmt.Println("\nRun 'llamawizard models add --link' for a guided tutorial.")
		os.Exit(1)
	}
}

func addFromHFResolve(st *state.State, repo, filename, name string) {
	// Look up the file size (and any matching mmproj) so the download can show
	// a percentage. Best-effort: if the tree lookup fails, download anyway with
	// unknown total.
	main := download.RemoteFile{RepoID: repo, Filename: filename}
	var mmprojs []download.RemoteFile
	if files, err := download.ListGGUFFiles(repo); err == nil {
		for _, f := range files {
			if f.Filename == filename {
				main.Size = f.Size
			}
			if f.IsMmproj {
				mmprojs = append(mmprojs, f)
			}
		}
	}

	res := runHFAddTUI(repo, name, []download.RemoteFile{main}, mmprojs, true)
	if !res.ok {
		fmt.Println("\nCancelled. Any partially downloaded file will resume next time.")
		return
	}

	var mmprojName string
	if res.mmproj != nil {
		mmprojName = res.mmproj.Filename
	}
	home, _ := os.UserHomeDir()
	size := fileSize(filepath.Join(home, "models", res.slug, res.main.Filename))
	registerModel(st, res.slug, name, repo, res.main.Filename, mmprojName, size)
}

func addFromHFRepo(st *state.State, repo, name string) {
	files, err := download.ListGGUFFiles(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not list files for %s: %v\n", repo, err)
		fmt.Println("\nMake sure the URL points to a valid HuggingFace model repo.")
		os.Exit(1)
	}

	var mains, mmprojs []download.RemoteFile
	for _, f := range files {
		if f.IsMmproj {
			mmprojs = append(mmprojs, f)
		} else {
			mains = append(mains, f)
		}
	}
	if len(mains) == 0 {
		fmt.Fprintf(os.Stderr, "No .gguf model files found in %s.\n", repo)
		fmt.Println("\nUse a direct file link instead, e.g.:")
		fmt.Printf("  https://huggingface.co/%s/resolve/main/<file>.gguf\n", repo)
		os.Exit(1)
	}

	res := runHFAddTUI(repo, name, mains, mmprojs, false)
	if !res.ok {
		fmt.Println("\nCancelled. Any partially downloaded file will resume next time.")
		return
	}

	var mmprojName string
	if res.mmproj != nil {
		mmprojName = res.mmproj.Filename
	}
	home, _ := os.UserHomeDir()
	size := fileSize(filepath.Join(home, "models", res.slug, res.main.Filename))
	registerModel(st, res.slug, name, repo, res.main.Filename, mmprojName, size)
}

func addFromDirect(st *state.State, rawURL, filename, name string) {
	slug := deriveSlug(name, filename)
	home, _ := os.UserHomeDir()
	destDir := filepath.Join(home, "models", slug)

	fmt.Printf("Downloading %s...\n", filename)
	if err := download.DownloadURL(rawURL, filename, 0, destDir, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		fmt.Println("Any partially downloaded file will resume next time.")
		os.Exit(1)
	}

	destPath := filepath.Join(destDir, filename)
	if err := validateGGUF(destPath); err != nil {
		fmt.Fprintf(os.Stderr, "Downloaded file rejected: %v\n", err)
		os.Exit(1)
	}

	size := fileSize(destPath)
	fmt.Printf("Downloaded %s (%.1f MB)\n", filename, float64(size)/(1024*1024))
	registerModel(st, slug, name, "", filename, "", size)
}

func pickMmproj(mmprojs []download.RemoteFile, mainFile string) *download.RemoteFile {
	if len(mmprojs) == 0 {
		return nil
	}
	quant := quantFromFilename(mainFile)
	if quant != "" {
		for i := range mmprojs {
			if strings.Contains(strings.ToUpper(mmprojs[i].Filename), quant) {
				return &mmprojs[i]
			}
		}
	}
	return &mmprojs[0]
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// slugExists reports whether a model with this slug is already in state.
// registerModel consults it before saving: a duplicate entry would persist
// to state.json first, and llamaswap.GenerateConfig would then reject every
// subsequent config regeneration (models remove, wizard, updateAllConfig)
// until the duplicate is cleaned up.
func slugExists(st *state.State, slug string) bool {
	for _, m := range st.Models {
		if m.Slug == slug {
			return true
		}
	}
	return false
}

func registerModel(st *state.State, slug, name, repo, filename, mmproj string, size int64) {
	if slugExists(st, slug) {
		fmt.Fprintf(os.Stderr, "\nModel '%s' is already installed.\n", slug)
		fmt.Println("Remove it first with: llamawizard models remove " + slug)
		os.Exit(1)
	}

	quant := quantFromFilename(filename)
	if quant == "" {
		quant = "custom"
	}
	displayName := name
	if displayName == "" {
		displayName = slug
	}

	st.Models = append(st.Models, state.ModelEntry{
		Slug:        slug,
		Name:        displayName,
		HFRepo:      repo,
		Quant:       quant,
		File:        filename,
		Mmproj:      mmproj,
		SizeBytes:   size,
		InstalledAt: time.Now().Format(time.RFC3339),
	})
	if err := st.Save(""); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving state: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nModel '%s' added successfully.\n", slug)

	if confirm("\nUpdate all configuration? (llama-swap config + pi) [Y/n] ") {
		updateAllConfig(st, slug)
	} else {
		fmt.Println("Configuration not updated — the model is saved but not yet active.")
		fmt.Println("Run 'llamawizard models add' (interactive) to update configuration.")
	}

	fmt.Println("Run 'llamawizard status' to verify.")
}

// updateAllConfig regenerates the llama-swap config and, when pi is present,
// reconfigures pi with the current models and the given default model.
func updateAllConfig(st *state.State, defaultSlug string) {
	regenerateConfig(st)

	if pi.IsInstalled() || st.PiConfigured {
		if err := pi.ConfigureModels(st.Port, st.Models); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pi model config failed: %v\n", err)
		} else if err := pi.ConfigureSettings(defaultSlug, st.Models); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pi settings failed: %v\n", err)
		} else {
			fmt.Printf("pi configuration updated (default model: %s).\n", defaultSlug)
			st.PiConfigured = true
			_ = st.Save("")
		}
	}

	restartServiceAfterAdd()
}

// confirm prompts the user for a yes/no answer, defaulting to yes on empty
// input.
func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "" || input == "y" || input == "yes"
}

func restartServiceAfterAdd() {
	plistPath, err := defaultPlistPath()
	if err == nil {
		if err := launchd.Stop(); err != nil {
			log.Printf("Warning: failed to stop service: %v", err)
		}
		if err := launchd.Start(plistPath); err != nil {
			log.Printf("Warning: failed to start service: %v", err)
			fmt.Println("Service may not have restarted — check 'llamawizard status'")
		} else {
			fmt.Println("Service restarted.")
		}
	}
}
