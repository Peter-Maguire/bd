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
	versionInfo := Version{Version: version, Commit: commit, Date: date}
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

	slog.Info("Starting", slog.String("ver", versionInfo.Version),
		slog.String("date", versionInfo.Date), slog.String("commit", versionInfo.Commit))

	db, dbCloser, errDB := store.CreateDB(settingsMgr.DBPath())
	if errDB != nil {
		slog.Error("failed to create database", errAttr(errDB))
		return 1
	}
	defer dbCloser()

	rcon := newRconConnection(settings.Rcon.String(), settings.Rcon.Password)

	state := newGameState(db, settingsMgr, newPlayerStates(), rcon, db)

	parser := newLogParser()
	broadcaster := newEventBroadcaster()

	var logSrc backgroundService
	if settings.UDPListenerEnabled {
		ingest, errListener := newUDPListener(settings.UDPListenerAddr, parser, broadcaster)
		if errListener != nil {
			slog.Error("failed to start udp log listener", errAttr(errListener))
			return 1
		}
		logSrc = ingest
	} else {
		ingest, errLogReader := newLogIngest(filepath.Join(settings.TF2Dir, "console.log"), parser, true, broadcaster)
		if errLogReader != nil {
			slog.Error("Failed to create log startEventEmitter", errAttr(errLogReader))
			return 1
		}
		logSrc = ingest
	}

	cr := newChatRecorder(db, broadcaster)

	broadcaster.registerConsumer(state.eventChan, EvtAny)

	dataSource, errDataSource := newDataSource(settings)
	if errDataSource != nil {
		slog.Error("failed to create data source", errAttr(errDataSource))
		return 1
	}

	re := createRulesEngine(settingsMgr)

	cache, cacheErr := NewCache(settingsMgr.ConfigRoot(), DurationCacheTimeout)
	if cacheErr != nil {
		slog.Error("Failed to set up cache", errAttr(cacheErr))
		return 1
	}

	lm := newListManager(cache, re, settingsMgr)
	updater := newPlayerDataLoader(db, dataSource, settingsMgr, re, state.profileUpdateQueue, state.playerDataChan)
	discordPresence := newDiscordState(state, settingsMgr)
	processHandler := newProcessState(plat, rcon, settingsMgr)
	statusHandler := newStatusUpdater(rcon, processHandler, state, time.Second*2)
	bigBrotherHandler := newOverwatch(settingsMgr, rcon, state)

	mux, errRoutes := createHandlers(db, state, processHandler, settingsMgr, re, rcon)
	if errRoutes != nil {
		slog.Error("failed to create http handlers", errAttr(errRoutes))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := newHTTPServer(ctx, settings.HTTPListenAddr, mux)

	// Start all the background workers
	for _, svc := range []backgroundService{discordPresence, cr, logSrc, updater, statusHandler, bigBrotherHandler, processHandler, state, lm} {
		go svc.start(ctx)
	}

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

		tray := newAppSystray(plat, settingsMgr, processHandler)

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
