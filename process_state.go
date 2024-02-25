package main

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/leighmacdonald/bd/addons"
	"github.com/leighmacdonald/bd/platform"
)

type processState struct {
	gameProcessActive  atomic.Bool
	gameHasStartedOnce atomic.Bool
	sm                 *settingsManager
	rcon               rconConnection
	platform           platform.Platform
}

func newProcessState(platform platform.Platform, rcon rconConnection, sm *settingsManager) *processState {
	isRunning, _ := platform.IsGameRunning()

	ps := &processState{
		gameProcessActive:  atomic.Bool{},
		gameHasStartedOnce: atomic.Bool{},
		sm:                 sm,
		platform:           platform,
		rcon:               rcon,
	}

	ps.gameProcessActive.Store(isRunning)
	ps.gameHasStartedOnce.Store(isRunning)

	return ps
}

// launchGame is the main entry point to launching the game. It will install the included addon, write the
// voice bans out if enabled and execute the platform specific launcher command, blocking until exit.
func (p *processState) launchGame(settingsMgr *settingsManager) {
	settings := settingsMgr.Settings()

	if errInstall := addons.Install(settings.TF2Dir); errInstall != nil {
		slog.Error("Error trying to install addon", errAttr(errInstall))
	}

	args, errArgs := getLaunchArgs(
		settings.Rcon.Password,
		settings.Rcon.Port,
		settingsMgr.locateSteamDir(),
		settings.SteamID)

	if errArgs != nil {
		slog.Error("Failed to get TF2 launch args", errAttr(errArgs))

		return
	}
	// TODO Move outside of here
	// if settings.VoiceBansEnabled {
	//	if errVB := rules.ExportVoiceBans(settings.TF2Dir, settings.KickTags); errVB != nil {
	//		slog.Error("Failed to export voiceban list", errAttr(errVB))
	//	}
	// }

	if errLaunch := p.platform.LaunchTF2(settings.TF2Dir, args); errLaunch != nil {
		slog.Error("Failed to launch game", errAttr(errLaunch))
	} else {
		p.gameHasStartedOnce.Store(true)
	}
}

func (p *processState) Quit(ctx context.Context) error {
	if !p.gameProcessActive.Load() {
		return errNotMarked
	}

	_, err := p.rcon.exec(ctx, "quit", false)
	if err != nil {
		return err
	}

	return nil
}

// processChecker handles checking and updating the running state of the tf2 process.
func (p *processState) start(ctx context.Context) {
	ticker := time.NewTicker(DurationProcessTimeout)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			existingState := p.gameProcessActive.Load()

			newState, errRunningStatus := p.platform.IsGameRunning()
			if errRunningStatus != nil {
				slog.Error("Failed to get process run status", errAttr(errRunningStatus))

				continue
			}

			if existingState != newState {
				p.gameProcessActive.Store(newState)
				slog.Info("Game process state changed", slog.Bool("is_running", newState))
			}

			// Handle auto closing the app on game close if enabled
			if !p.gameHasStartedOnce.Load() || !p.sm.Settings().AutoCloseOnGameExit {
				continue
			}

			if !newState {
				slog.Info("Auto-closing on game exit")
				os.Exit(0)
			}
		}
	}
}
