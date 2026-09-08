// Package service verifies API readiness after launchd accepts a restart.
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eduard-lt/llamawizard/internal/health"
	"github.com/eduard-lt/llamawizard/internal/launchd"
	"github.com/eduard-lt/llamawizard/internal/llamaswap"
	"github.com/eduard-lt/llamawizard/internal/state"
)

var restart = launchd.Restart
var check = health.CheckWithKey

func RestartAndCheck(st *state.State) error {
	data, err := os.ReadFile(state.DefaultConfigPath())
	if err != nil {
		return err
	}
	_, key, err := llamaswap.Catalog(data)
	if err != nil {
		return err
	}
	ids, err := llamaswap.ListedModelIDs(data)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err = restart(filepath.Join(home, "Library", "LaunchAgents", launchd.PlistName)); err != nil {
		return err
	}
	report, err := check(st.Port, ids, key)
	if err != nil {
		return err
	}
	if !report.Pass {
		return fmt.Errorf("service is not ready: %s; missing models: %s\n%s", report.Error, strings.Join(report.MissingModels, ", "), report.ErrorLogTail)
	}
	return nil
}
