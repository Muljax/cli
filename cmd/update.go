package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/muljax/cli/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	// Default GitHub repository for Muljax CLI releases
	releaseRepo = "Muljax/cli"

	// HTTP client with reasonable timeout
	httpClient = &http.Client{
		Timeout: 60 * time.Second,
	}
)

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func NewUpdateCmd() *cobra.Command {
	var targetDir string
	var userMode bool
	var force bool
	var checkOnly bool
	var specificVersion string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update muljax to the latest release",
		Long: `Fetches the appropriate versioned archive for your platform from the latest 
Muljax release on GitHub, verifies its integrity, extracts it, and installs it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			goos := runtime.GOOS
			goarch := runtime.GOARCH

			if !isSupportedPlatform(goos, goarch) {
				return fmt.Errorf("unsupported platform %s/%s; supported platforms: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, windows/arm64", goos, goarch)
			}

			ui.Header("Muljax CLI Update")
			ui.KeyValue("Current Version", Version)
			ui.KeyValue("Platform", fmt.Sprintf("%s/%s", goos, goarch))

			// Resolve target release info
			targetTag, assets, err := resolveReleaseInfo(specificVersion)
			if err != nil {
				return fmt.Errorf("failed to fetch release information: %w", err)
			}

			targetVersion := strings.TrimPrefix(targetTag, "v")
			currentClean := strings.TrimPrefix(Version, "v")

			ui.KeyValue("Target Version", targetTag)
			fmt.Println()

			isUpToDate := currentClean == targetVersion && Version != "dev"

			if checkOnly {
				if isUpToDate {
					ui.Step(fmt.Sprintf("muljax is up to date (%s)", ui.Cyan(Version)))
				} else {
					ui.StepInfo(fmt.Sprintf("Update available: %s -> %s", ui.Dim(Version), ui.Cyan(targetTag)))
					fmt.Printf("Run '%s' to install the update.\n", ui.Cyan("muljax update"))
				}
				return nil
			}

			if isUpToDate && !force {
				ui.Step(fmt.Sprintf("muljax is already up to date (%s)", ui.Cyan(Version)))
				fmt.Printf("Use '%s' to force reinstallation.\n", ui.Cyan("muljax update --force"))
				return nil
			}

			// Determine archive filenames and URLs
			ext := ".tar.gz"
			if goos == "windows" {
				ext = ".zip"
			}

			// 1. Reproducible unversioned template: muljax_<os>_<arch>.<ext>
			reproducibleName := fmt.Sprintf("muljax_%s_%s%s", goos, goarch, ext)
			// 2. Legacy versioned template: muljax_<version>_<os>_<arch>.<ext>
			versionedName := fmt.Sprintf("muljax_%s_%s_%s%s", targetVersion, goos, goarch, ext)

			archiveDownloadURL, archiveName := resolveAssetURL(assets, targetTag, reproducibleName, versionedName)
			checksumsURL := resolveChecksumsURL(assets, targetTag)

			// Create temp work directory
			tmpDir, err := os.MkdirTemp("", "muljax-update-*")
			if err != nil {
				return fmt.Errorf("failed to create temporary working directory: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			// Download the archive
			archivePath := filepath.Join(tmpDir, archiveName)
			ui.StepInfo(fmt.Sprintf("Downloading %s...", ui.Cyan(archiveName)))
			if err := downloadFile(archiveDownloadURL, archivePath); err != nil {
				return fmt.Errorf("failed to download release archive: %w", err)
			}

			// Verify checksum if checksums.txt is available
			if checksumsURL != "" {
				checksumsPath := filepath.Join(tmpDir, "checksums.txt")
				if err := downloadFile(checksumsURL, checksumsPath); err == nil {
					ui.StepInfo("Verifying archive integrity...")
					if err := verifyChecksum(archivePath, checksumsPath, archiveName); err != nil {
						return fmt.Errorf("checksum verification failed: %w", err)
					}
					ui.Step("Archive integrity verified (SHA256)")
				} else {
					ui.StepWarn("Could not download checksums.txt; proceeding without checksum verification")
				}
			}

			// Extract archive
			ui.StepInfo("Extracting archive...")
			extractedBinPath, err := extractArchive(archivePath, tmpDir, goos)
			if err != nil {
				return fmt.Errorf("failed to extract archive: %w", err)
			}

			// Ensure binary is executable
			if err := os.Chmod(extractedBinPath, 0755); err != nil {
				return fmt.Errorf("failed to make extracted binary executable: %w", err)
			}

			// Construct arguments for the binary's install command
			installArgs := []string{"install", "--force"}
			if userMode {
				installArgs = append(installArgs, "--user")
			} else if targetDir != "" {
				installArgs = append(installArgs, "--dir", targetDir)
			} else {
				// If currently running from an installed directory in PATH, target that directory
				selfPath, err := os.Executable()
				if err == nil {
					if resolved, err := filepath.EvalSymlinks(selfPath); err == nil {
						selfPath = resolved
					}
					selfDir := filepath.Dir(selfPath)
					if isDirInPath(selfDir) {
						installArgs = append(installArgs, "--dir", selfDir)
					}
				}
			}

			if noColor || ui.NoColor {
				installArgs = append(installArgs, "--no-color")
			}

			// Run the extracted binary's install command
			installCmd := exec.Command(extractedBinPath, installArgs...)
			installCmd.Stdout = os.Stdout
			installCmd.Stderr = os.Stderr
			installCmd.Stdin = os.Stdin

			if err := installCmd.Run(); err != nil {
				return fmt.Errorf("installation failed: %w", err)
			}

			ui.Step(fmt.Sprintf("Successfully updated muljax to %s", ui.Bold(targetTag)))
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetDir, "dir", "d", "", "custom directory to install binary into")
	cmd.Flags().BoolVarP(&userMode, "user", "u", false, "install into user local bin directory instead of system-wide")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "force update even if already up to date")
	cmd.Flags().BoolVarP(&checkOnly, "check", "c", false, "check for available updates without installing")
	cmd.Flags().StringVar(&specificVersion, "version", "", "specific release version to install (e.g. v0.7.1)")

	return cmd
}

func isSupportedPlatform(goos, goarch string) bool {
	switch goos {
	case "linux", "darwin", "windows":
		return goarch == "amd64" || goarch == "arm64"
	default:
		return false
	}
}

func resolveReleaseInfo(specificVersion string) (string, []struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}, error) {
	var apiURL string
	if specificVersion != "" {
		tag := specificVersion
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", releaseRepo, tag)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", releaseRepo)
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "muljax-cli-updater")

	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var rel githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil {
			return rel.TagName, rel.Assets, nil
		}
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	// Fallback when GitHub API is rate-limited or unavailable:
	// For "latest", follow redirect of https://github.com/<repo>/releases/latest
	if specificVersion == "" {
		tag, err := resolveLatestTagViaRedirect()
		if err != nil {
			return "", nil, fmt.Errorf("could not query GitHub API or resolve latest release tag: %w", err)
		}
		return tag, nil, nil
	}

	tag := specificVersion
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return tag, nil, nil
}

func resolveLatestTagViaRedirect() (string, error) {
	redirectClient := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("https://github.com/%s/releases/latest", releaseRepo)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "muljax-cli-updater")

	resp, err := redirectClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		if loc != "" {
			parts := strings.Split(loc, "/releases/tag/")
			if len(parts) == 2 {
				return parts[1], nil
			}
		}
	}

	return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
}

func resolveAssetURL(assets []struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}, tag, reproducibleName, versionedName string) (string, string) {
	// First look in API assets for reproducible unversioned name
	for _, a := range assets {
		if a.Name == reproducibleName {
			return a.BrowserDownloadURL, a.Name
		}
	}

	// Next look for versioned legacy name
	for _, a := range assets {
		if a.Name == versionedName {
			return a.BrowserDownloadURL, a.Name
		}
	}

	// Direct download URL fallback: check if reproducible name exists, otherwise use versioned
	directReproURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", releaseRepo, tag, reproducibleName)
	req, err := http.NewRequest(http.MethodHead, directReproURL, nil)
	if err == nil {
		resp, err := httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
				return directReproURL, reproducibleName
			}
		}
	}

	directVersionedURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", releaseRepo, tag, versionedName)
	return directVersionedURL, versionedName
}

func resolveChecksumsURL(assets []struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}, tag string) string {
	for _, a := range assets {
		if a.Name == "checksums.txt" {
			return a.BrowserDownloadURL
		}
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", releaseRepo, tag)
}

func getGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

func downloadFile(url, destPath string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "muljax-cli-updater")
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d downloading %s", resp.StatusCode, url)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func verifyChecksum(archivePath, checksumsPath, archiveName string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}
	actualChecksum := fmt.Sprintf("%x", hasher.Sum(nil))

	checksumData, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(checksumData), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		expectedHash := fields[0]
		fileName := strings.TrimPrefix(fields[1], "*")

		if fileName == archiveName || filepath.Base(fileName) == archiveName {
			if !strings.EqualFold(expectedHash, actualChecksum) {
				return fmt.Errorf("checksum mismatch for %s:\n  expected: %s\n  got:      %s", archiveName, expectedHash, actualChecksum)
			}
			return nil
		}
	}

	return fmt.Errorf("archive %s not found in checksums.txt", archiveName)
}

func extractArchive(archivePath, destDir, goos string) (string, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, destDir)
	}
	return extractTarGz(archivePath, destDir)
}

func extractTarGz(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	binName := "muljax"
	if runtime.GOOS == "windows" {
		binName = "muljax.exe"
	}
	var extractedBinPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "..") {
			continue // prevent zip slip
		}

		target := filepath.Join(destDir, cleanName)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()

			if filepath.Base(cleanName) == binName {
				_ = os.Chmod(target, 0755)
				extractedBinPath = target
			}
		}
	}

	if extractedBinPath == "" {
		return "", fmt.Errorf("binary %s not found in tar archive", binName)
	}
	return extractedBinPath, nil
}

func extractZip(archivePath, destDir string) (string, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	binName := "muljax"
	if runtime.GOOS == "windows" {
		binName = "muljax.exe"
	}
	var extractedBinPath string

	for _, f := range r.File {
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") {
			continue // prevent zip slip
		}

		target := filepath.Join(destDir, cleanName)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}

		rc, err := f.Open()
		if err != nil {
			return "", err
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return "", err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return "", err
		}

		if filepath.Base(cleanName) == binName {
			_ = os.Chmod(target, 0755)
			extractedBinPath = target
		}
	}

	if extractedBinPath == "" {
		return "", fmt.Errorf("binary %s not found in zip archive", binName)
	}
	return extractedBinPath, nil
}
