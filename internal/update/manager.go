package update

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	releasesEndpoint = "https://api.github.com/repos/Cybervaldemar/sidecast/releases?per_page=100"
	assetName        = "sidecast-windows-amd64.exe"
	checksumName     = "SHA256SUMS"
	maxExecutable    = 200 << 20
	maxChecksums     = 1 << 20
)

var ErrUnavailable = errors.New("automatic update is unavailable")
var ErrBusy = errors.New("update already in progress")

type State struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Available      bool   `json:"available"`
	CanUpdate      bool   `json:"canUpdate"`
	ReleaseURL     string `json:"releaseUrl"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

type release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type releaseInfo struct {
	version, page, binary, sums string
}

type Manager struct {
	mu          sync.Mutex
	checkMu     sync.Mutex
	client      *http.Client
	endpoint    string
	stop        func()
	executable  string
	args        []string
	workingDir  string
	canCheck    bool
	canInstall  bool
	checkedAt   time.Time
	checkError  error
	latest      releaseInfo
	status      string
	updateError string
}

func New(stop func()) *Manager {
	executable, _ := os.Executable()
	workingDir, _ := os.Getwd()
	_, validVersion := parseVersion(Version)
	return &Manager{
		client:     &http.Client{CheckRedirect: safeRedirect},
		endpoint:   releasesEndpoint,
		stop:       stop,
		executable: executable,
		args:       append([]string(nil), os.Args[1:]...),
		workingDir: workingDir,
		canCheck:   validVersion,
		canInstall: runtime.GOOS == "windows" && runtime.GOARCH == "amd64" && validVersion,
		status:     "idle",
	}
}

func safeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 8 || req.URL.Scheme != "https" {
		return errors.New("unsafe release redirect")
	}
	host := strings.ToLower(req.URL.Hostname())
	if host != "github.com" && host != "api.github.com" && host != "objects.githubusercontent.com" && host != "release-assets.githubusercontent.com" && host != "github-releases.githubusercontent.com" {
		return errors.New("unexpected release download host")
	}
	return nil
}

func (m *Manager) Check(ctx context.Context) (State, error) {
	if !m.canCheck {
		return State{CurrentVersion: Version, Status: "idle"}, nil
	}
	m.mu.Lock()
	busy := m.status != "idle"
	cached := time.Since(m.checkedAt) < 15*time.Minute
	m.mu.Unlock()
	if !busy && !cached {
		m.checkMu.Lock()
		m.mu.Lock()
		cached = time.Since(m.checkedAt) < 15*time.Minute
		busy = m.status != "idle"
		m.mu.Unlock()
		if !busy && !cached {
			latest, err := m.fetchLatest(ctx)
			m.mu.Lock()
			m.checkedAt, m.latest, m.checkError = time.Now(), latest, err
			m.mu.Unlock()
		}
		m.checkMu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.stateLocked()
	if m.checkError != nil && m.status == "idle" {
		return state, m.checkError
	}
	return state, nil
}

func (m *Manager) stateLocked() State {
	state := State{
		CurrentVersion: Version,
		LatestVersion:  m.latest.version,
		ReleaseURL:     m.latest.page,
		Status:         m.status,
		Error:          m.updateError,
	}
	current, currentOK := parseVersion(Version)
	latest, latestOK := parseVersion(m.latest.version)
	state.Available = currentOK && latestOK && compareVersion(latest, current) > 0
	state.CanUpdate = m.canInstall && state.Available && m.latest.binary != "" && m.latest.sums != ""
	return state
}

func (m *Manager) Start(ctx context.Context) error {
	state, err := m.Check(ctx)
	if err != nil {
		return fmt.Errorf("check release: %w", err)
	}
	if !state.CanUpdate {
		return ErrUnavailable
	}
	m.mu.Lock()
	if m.status == "downloading" || m.status == "restarting" {
		m.mu.Unlock()
		return ErrBusy
	}
	m.status, m.updateError = "downloading", ""
	selected := m.latest
	m.mu.Unlock()
	go m.install(selected)
	return nil
}

func (m *Manager) install(selected releaseInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := m.downloadAndRestart(ctx, selected); err != nil {
		m.mu.Lock()
		m.status = "failed"
		m.updateError = err.Error()
		m.mu.Unlock()
	}
}

func (m *Manager) fetchLatest(ctx context.Context) (releaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", m.endpoint, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Sidecast/"+Version)
	response, err := m.client.Do(request)
	if err != nil {
		return releaseInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("GitHub releases: HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, 2<<20)
	var releases []release
	if err := json.NewDecoder(limited).Decode(&releases); err != nil {
		return releaseInfo{}, fmt.Errorf("decode GitHub releases: %w", err)
	}
	var chosen releaseInfo
	var highest version
	for _, item := range releases {
		if item.Draft {
			continue
		}
		parsed, ok := parseVersion(item.TagName)
		if !ok || (chosen.version != "" && compareVersion(parsed, highest) <= 0) {
			continue
		}
		candidate := releaseInfo{version: strings.TrimPrefix(item.TagName, "v"), page: item.HTMLURL}
		for _, asset := range item.Assets {
			switch asset.Name {
			case assetName:
				if validAssetURL(asset.URL, item.TagName, assetName) {
					candidate.binary = asset.URL
				}
			case checksumName:
				if validAssetURL(asset.URL, item.TagName, checksumName) {
					candidate.sums = asset.URL
				}
			}
		}
		chosen, highest = candidate, parsed
	}
	return chosen, nil
}

func validAssetURL(raw, tag, name string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.EscapedPath() == "/Cybervaldemar/sidecast/releases/download/"+url.PathEscape(tag)+"/"+name
}

func (m *Manager) downloadAndRestart(ctx context.Context, selected releaseInfo) error {
	sums, err := m.readAsset(ctx, selected.sums, maxChecksums)
	if err != nil {
		return fmt.Errorf("загрузить контрольные суммы: %w", err)
	}
	expected, err := checksumFor(sums, "windows-amd64/"+assetName)
	if err != nil {
		return fmt.Errorf("проверить контрольные суммы: %w", err)
	}
	directory := filepath.Dir(m.executable)
	candidate, err := os.CreateTemp(directory, ".sidecast-update-*.exe")
	if err != nil {
		return fmt.Errorf("нет доступа к папке приложения: %w", err)
	}
	candidatePath := candidate.Name()
	launched := false
	defer func() {
		if !launched {
			_ = os.Remove(candidatePath)
		}
	}()
	if err := m.downloadFile(ctx, selected.binary, candidate, expected); err != nil {
		_ = candidate.Close()
		return err
	}
	if err := candidate.Close(); err != nil {
		return err
	}
	helperPath := strings.TrimSuffix(candidatePath, ".exe") + "-helper.exe"
	if err := copyFile(candidatePath, helperPath); err != nil {
		return fmt.Errorf("подготовить перезапуск: %w", err)
	}
	defer func() {
		// The running helper can only be removed by the new process.
		if !launched {
			_ = os.Remove(helperPath)
		}
	}()
	payload := applyPayload{
		Target: m.executable, Candidate: candidatePath,
		Backup: candidatePath + ".old", Ready: candidatePath + ".ready",
		Started: candidatePath + ".started", PID: os.Getpid(),
		Arguments: m.args, WorkingDir: m.workingDir,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	command := exec.Command(helperPath, "--apply-update")
	command.Dir = m.workingDir
	command.Env = append(os.Environ(), "SIDECAST_UPDATE_PAYLOAD="+string(encoded))
	if err := command.Start(); err != nil {
		return fmt.Errorf("запустить установщик: %w", err)
	}
	defer os.Remove(payload.Started)
	if err := waitForHelperReady(ctx, command.Process.Pid, payload.Started); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("установщик не запустился: %w", err)
	}
	launched = true
	_ = command.Process.Release()
	m.mu.Lock()
	m.status = "restarting"
	m.mu.Unlock()
	m.stop()
	return nil
}

func (m *Manager) readAsset(ctx context.Context, raw string, limit int64) ([]byte, error) {
	response, err := m.assetResponse(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release asset exceeds size limit")
	}
	return data, nil
}

func (m *Manager) assetResponse(ctx context.Context, raw string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Sidecast/"+Version)
	response, err := m.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("release asset: HTTP %d", response.StatusCode)
	}
	return response, nil
}

func (m *Manager) downloadFile(ctx context.Context, raw string, file *os.File, expected [sha256.Size]byte) error {
	response, err := m.assetResponse(ctx, raw)
	if err != nil {
		return fmt.Errorf("загрузить обновление: %w", err)
	}
	defer response.Body.Close()
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxExecutable+1))
	if err != nil {
		return fmt.Errorf("загрузить обновление: %w", err)
	}
	if count == 0 || count > maxExecutable {
		return errors.New("размер обновления превышает допустимый")
	}
	if !equalHash(hash.Sum(nil), expected[:]) {
		return errors.New("контрольная сумма обновления не совпала")
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("сохранить обновление: %w", err)
	}
	return nil
}

func equalHash(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for index := range left {
		result |= left[index] ^ right[index]
	}
	return result == 0
}

func checksumFor(data []byte, filename string) ([sha256.Size]byte, error) {
	var found [sha256.Size]byte
	matched := false
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[1] != filename {
			continue
		}
		if matched {
			return found, errors.New("duplicate checksum entry")
		}
		decoded, err := hex.DecodeString(parts[0])
		if err != nil || len(decoded) != sha256.Size {
			return found, errors.New("invalid SHA-256 entry")
		}
		copy(found[:], decoded)
		matched = true
	}
	if !matched {
		return found, fmt.Errorf("missing SHA-256 for %s", filename)
	}
	return found, nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		os.Remove(target)
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		os.Remove(target)
		return err
	}
	if err := output.Close(); err != nil {
		os.Remove(target)
		return err
	}
	return nil
}

type applyPayload struct {
	Target     string   `json:"target"`
	Candidate  string   `json:"candidate"`
	Backup     string   `json:"backup"`
	Ready      string   `json:"ready"`
	Started    string   `json:"started"`
	PID        int      `json:"pid"`
	Arguments  []string `json:"arguments"`
	WorkingDir string   `json:"workingDir"`
}

func applyPayloadFromEnvironment() (applyPayload, error) {
	var payload applyPayload
	if err := json.Unmarshal([]byte(os.Getenv("SIDECAST_UPDATE_PAYLOAD")), &payload); err != nil {
		return payload, err
	}
	if payload.PID <= 0 || !filepath.IsAbs(payload.Target) || !filepath.IsAbs(payload.Candidate) || !filepath.IsAbs(payload.Backup) || !filepath.IsAbs(payload.Ready) || !filepath.IsAbs(payload.Started) || !strings.EqualFold(filepath.Ext(payload.Target), ".exe") ||
		filepath.Dir(payload.Target) != filepath.Dir(payload.Candidate) || filepath.Dir(payload.Target) != filepath.Dir(payload.Backup) || filepath.Dir(payload.Target) != filepath.Dir(payload.Ready) ||
		filepath.Dir(payload.Target) != filepath.Dir(payload.Started) || payload.Candidate == payload.Backup || payload.Target == payload.Backup || payload.Target == payload.Candidate ||
		payload.Ready != payload.Candidate+".ready" || payload.Started != payload.Candidate+".started" {
		return payload, errors.New("invalid update target")
	}
	return payload, nil
}

func updatedEnvironment(helper, ready string) []string {
	var env []string
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "SIDECAST_UPDATE_PAYLOAD=") && !strings.HasPrefix(item, "SIDECAST_UPDATE_READY=") &&
			!strings.HasPrefix(item, "SIDECAST_UPDATE_CLEANUP=") && !strings.HasPrefix(item, "SIDECAST_UPDATE_RESTART=") {
			env = append(env, item)
		}
	}
	return append(env, "SIDECAST_UPDATE_RESTART=1", "SIDECAST_UPDATE_CLEANUP="+helper, "SIDECAST_UPDATE_READY="+ready)
}

// Restarted reports whether this launch followed a successful update. The
// already-open studio page can reconnect without opening another browser tab.
func Restarted() bool { return os.Getenv("SIDECAST_UPDATE_RESTART") == "1" }

// WaitAndSignalReady confirms that the new server responds before allowing the
// helper to discard the previous executable.
func WaitAndSignalReady(ctx context.Context, studio string) error {
	path := os.Getenv("SIDECAST_UPDATE_READY")
	if path == "" {
		return nil
	}
	self, err := os.Executable()
	if err != nil || filepath.Dir(path) != filepath.Dir(self) || !strings.HasPrefix(filepath.Base(path), ".sidecast-update-") || !strings.HasSuffix(path, ".exe.ready") {
		return errors.New("invalid update readiness marker")
	}
	if studio == "" {
		return errors.New("local studio address is unavailable")
	}
	// This request stays on this computer and carries no credentials. A custom
	// HTTPS certificate may be issued for the public hostname instead of localhost.
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	defer client.CloseIdleConnections()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	address := strings.TrimSuffix(studio, "/studio") + "/api/health"
	for {
		request, err := http.NewRequestWithContext(ctx, "GET", address, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			var health struct{ Status, Version string }
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&health)
			response.Body.Close()
			if decodeErr == nil && response.StatusCode == http.StatusOK && health.Status == "ok" && health.Version == Version {
				return os.WriteFile(path, []byte("ready"), 0600)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("new Sidecast did not respond to health checks")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// CleanupHelper removes the short-lived helper after it exits. Windows cannot
// delete an executable while that executable is still running.
func CleanupHelper() {
	path := os.Getenv("SIDECAST_UPDATE_CLEANUP")
	self, err := os.Executable()
	if err != nil || filepath.Dir(path) != filepath.Dir(self) || !strings.HasPrefix(filepath.Base(path), ".sidecast-update-") || !strings.HasSuffix(path, "-helper.exe") {
		return
	}
	go func() {
		for range 60 {
			if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
				candidate := strings.TrimSuffix(path, "-helper.exe") + ".exe"
				_ = os.Remove(candidate)
				_ = os.Remove(candidate + ".old")
				_ = os.Remove(candidate + ".ready")
				_ = os.Remove(candidate + ".started")
				return
			}
			time.Sleep(time.Second)
		}
	}()
}
