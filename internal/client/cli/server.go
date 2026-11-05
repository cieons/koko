package cli

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
)

var serverCommands = []Command{
	{
		Noun:    "server",
		Verb:    "run",
		Summary: "Run the koko server in the foreground, as its systemd unit does",
		Flags: []Flag{
			{
				Name:    "data-dir",
				Value:   "dir",
				Env:     "KOKO_DATA_DIR",
				Default: "/var/lib/koko",
				Summary: "the data directory, which holds koko.db",
			},
			{
				Name:    "log-level",
				Value:   "level",
				Env:     "KOKO_LOG_LEVEL",
				Default: "info",
				Choices: []string{"debug", "info", "warn", "error"},
				Summary: "the lowest level the log on stderr holds",
			},
		},
		// It runs the koko server and migrates its database (P0 spec: overview).
		Tier: Irreversible,
		Run:  quietAction(serverRun),
	},
}

func serverRun(ctx context.Context, in *Input) error {
	if in.runner.ServerRun == nil {
		return &cliError{code: codeUnsupportedPlatform, message: fmt.Sprintf(
			"the koko server runs on Linux only; this koko is built for %s/%s", runtime.GOOS, runtime.GOARCH)}
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(in.Flag("log-level"))); err != nil {
		return fmt.Errorf("read the log level: %w", err)
	}
	return in.runner.ServerRun(ctx, ServerSettings{DataDir: in.Flag("data-dir"), LogLevel: level}, in.inv.Stderr)
}
