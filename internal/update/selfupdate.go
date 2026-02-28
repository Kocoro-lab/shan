package update

import (
	"context"
	"fmt"
	"runtime"

	"github.com/creativeprojects/go-selfupdate"
)

const repoOwner = "Kocoro-lab"
const repoName = "shannon-cli"

func CheckForUpdate(currentVersion string) (*selfupdate.Release, bool, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, false, err
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
	if err != nil {
		return nil, false, err
	}

	release, found, err := updater.DetectLatest(
		context.Background(),
		selfupdate.NewRepositorySlug(repoOwner, repoName),
	)
	if err != nil || !found {
		return nil, false, err
	}

	if release.LessOrEqual(currentVersion) {
		return nil, false, nil
	}

	return release, true, nil
}

func DoUpdate(currentVersion string) (string, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return "", err
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
	if err != nil {
		return "", err
	}

	release, found, err := updater.DetectLatest(
		context.Background(),
		selfupdate.NewRepositorySlug(repoOwner, repoName),
	)
	if err != nil {
		return "", err
	}
	if !found || release.LessOrEqual(currentVersion) {
		return currentVersion, fmt.Errorf("already up to date (%s)", currentVersion)
	}

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), release, exe); err != nil {
		return "", fmt.Errorf("update failed: %w", err)
	}

	return release.Version(), nil
}

func PlatformInfo() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
