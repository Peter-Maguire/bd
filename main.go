package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/leighmacdonald/bd/platform"
	"github.com/leighmacdonald/bd/rules"
	"github.com/leighmacdonald/bd/store"
	_ "modernc.org/sqlite"
)

var (
	// Build info embedded at build time.
	version = "master" //nolint:gochecknoglobals
	commit  = "latest" //nolint:gochecknoglobals
	date    = "n/a"    //nolint:gochecknoglobals
	builtBy = "src"    //nolint:gochecknoglobals
)

func createRulesEngine(sm *settingsManager) *rules.Engine {
	rulesEngine := rules.New()

	if sm.Settings().RunMode != ModeTest { //nolint:nestif
		// Try and load our existing custom players
		if platform.Exists(sm.LocalPlayerListPath()) {
			input, errInput := os.Open(sm.LocalPlayerListPath())
			if errInput != nil {
				slog.Error("Failed to open local player list", errAttr(errInput))
			} else {
				var localPlayersList rules.PlayerListSchema
				if errRead := json.NewDecoder(input).Decode(&localPlayersList); errRead != nil {
					slog.Error("Failed to parse local player list", errAttr(errRead))
				} else {
					count, errPlayerImport := rulesEngine.ImportPlayers(&localPlayersList)
					if errPlayerImport != nil {
						slog.Error("Failed to import local player list", errAttr(errPlayerImport))
					} else {
						slog.Info("Loaded local player list", slog.Int("count", count))
					}
				}

				LogClose(input)
			}
		}

		// Try and load our existing custom rules
		if platform.Exists(sm.LocalRulesListPath()) {
			input, errInput := os.Open(sm.LocalRulesListPath())
			if errInput != nil {
				slog.Error("Failed to open local rules list", errAttr(errInput))
			} else {
				var localRules rules.RuleSchema
				if errRead := json.NewDecoder(input).Decode(&localRules); errRead != nil {
					slog.Error("Failed to parse local rules list", errAttr(errRead))
				} else {
					count, errRulesImport := rulesEngine.ImportRules(&localRules)
					if errRulesImport != nil {
						slog.Error("Failed to import local rules list", errAttr(errRulesImport))
					}

					slog.Debug("Loaded local rules list", slog.Int("count", count))
				}

				LogClose(input)
			}
		}
	}

	return rulesEngine
}

// openApplicationPage launches the http frontend using the platform specific browser launcher function.
func openApplicationPage(plat platform.Platform, appURL string) {
	if errOpen := plat.OpenURL(appURL); errOpen != nil {
		slog.Error("Failed to open URL", slog.String("url", appURL), errAttr(errOpen))
	}
}

func run() int {
	versionInfo := Version{Version: version, Commit: commit, Date: date, BuiltBy: builtBy}
	plat := platform.New()
	settingsMgr := newSettingsManager(plat)

	if errSetup := settingsMgr.setup(); errSetup != nil {
		slog.Error("Failed to create settings directories", errAttr(errSetup))

		return 1
	}

	if errSettings := settingsMgr.validateAndLoad(); errSettings != nil {
		slog.Error("Failed to load settings", errAttr(errSettings))

		return 1
	}

	settings := settingsMgr.Settings()

	logCloser := MustCreateLogger(settingsMgr)
	defer logCloser()

	slog.Info("Starting BD",
		slog.String("version", versionInfo.Version),
		slog.String("date", versionInfo.Date),
		slog.String("commit", versionInfo.Commit),
		slog.String("via", versionInfo.BuiltBy))

	db, dbCloser, errDB := store.CreateDB(settingsMgr.DBPath())
	if errDB != nil {
		slog.Error("failed to create database", errAttr(errDB))
		return 1
	}
	defer dbCloser()

	// fsCache, cacheErr := NewCache(settingsMgr.ConfigRoot(), DurationCacheTimeout)
	// if cacheErr != nil {
	//	 slog.Error("Failed to set up cache", errAttr(cacheErr))
	//	 return 1
	// }

	rcon := newRconConnection(settings.Rcon.String(), settings.Rcon.Password)
	state := newGameState(db, settingsMgr, newPlayerStates(), rcon)
	eh := newEventHandler(state)

	ingest, errLogReader := newLogIngest(filepath.Join(settings.TF2Dir, "console.log"), newLogParser(), true)
	if errLogReader != nil {
		slog.Error("Failed to create log startEventEmitter", errAttr(errLogReader))
		return 1
	}

	cr := newChatRecorder(db, ingest)

	ingest.registerConsumer(eh.eventChan)

	dataSource, errDataSource := newDataSource(settings)
	if errDataSource != nil {
		slog.Error("failed to create data source", errAttr(errDataSource))
		return 1
	}

	updater := newProfileUpdater(db, dataSource, state, settingsMgr)
	discordPresence := newDiscordState(state, settingsMgr)
	re := createRulesEngine(settingsMgr)
	process := newProcessState(plat, rcon)
	su := newStatusUpdater(rcon, process, state, time.Second*2)
	bb := newBigBrother(settingsMgr, rcon, state)

	mux, errRoutes := createHandlers(db, state, process, settingsMgr, re, rcon)
	if errRoutes != nil {
		slog.Error("failed to create http handlers", errAttr(errRoutes))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := newHTTPServer(ctx, settings.HTTPListenAddr, mux)

	// Start all the background workers
	go eh.start(ctx)
	go discordPresence.start(ctx)
	go cr.start(ctx)
	go ingest.start(ctx)
	go updater.start(ctx)
	go su.start(ctx)
	go bb.start(ctx)
	go testLogFeeder(ctx, ingest)

	go func() {
		// TODO configure the auto open
		time.Sleep(time.Second * 3)

		if settings.RunMode == ModeRelease {
			openApplicationPage(plat, settings.AppURL())
		}
	}()

	go func() {
		if errServe := httpServer.ListenAndServe(); errServe != nil && !errors.Is(errServe, http.ErrServerClosed) {
			slog.Error("error trying to shutdown http service", errAttr(errServe))
		}
	}()

	if settings.SystrayEnabled {
		slog.Debug("Using systray")

		tray := newAppSystray(plat, settingsMgr, process)

		systray.Run(tray.OnReady(ctx), func() {
			stop()
		})
	}

	<-ctx.Done()

	timeout, cancelHTTP := context.WithTimeout(context.Background(), time.Second*15)
	defer cancelHTTP()

	if errShutdown := httpServer.Shutdown(timeout); errShutdown != nil {
		slog.Error("Failed to shutdown cleanly", errAttr(errShutdown))
	} else {
		slog.Debug("HTTP Service shutdown successfully")
	}

	return 0
}

func main() {
	os.Exit(run())
}
